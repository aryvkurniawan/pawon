// Command pawon: panel kontrol Windows (web UI embedded + supervisor
// service stack). Console (default) | service install | service remove |
// service run (dipanggil SCM).
package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	versions "pawon/internal"
	"pawon/internal/cf"
	"pawon/internal/db"
	"pawon/internal/dl"
	"pawon/internal/hosts"
	"pawon/internal/nginx"
	"pawon/internal/php"
	"pawon/internal/proc"
	"pawon/internal/server"
	"pawon/internal/sites"
	"pawon/internal/state"
	"pawon/internal/svc"
	"pawon/internal/tunnel"
)

//go:embed all:web
var webFS embed.FS

const panelAddr = "127.0.0.1:7080"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pawon:", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	switch {
	case svc.IsWindowsService() || (len(args) == 2 && args[0] == "service" && args[1] == "run"):
		return svc.RunService(func(stop chan struct{}) {
			if err := panel(stop); err != nil {
				fmt.Fprintln(os.Stderr, "pawon:", err)
			}
		})
	case len(args) == 2 && args[0] == "service" && args[1] == "install":
		return svc.Install()
	case len(args) == 2 && args[0] == "service" && args[1] == "remove":
		return svc.Remove()
	case len(args) == 0:
		return console()
	}
	usage()
	return errors.New("argumen tidak dikenal")
}

func usage() {
	fmt.Fprint(os.Stderr, `pakai: pawon.exe                  # console: init stack + start service + UI
       pawon.exe service install  # daftarkan Windows service (auto-start)
       pawon.exe service remove   # hapus service
       pawon.exe service run      # dipanggil SCM; jangan dipanggil manual
`)
}

// console: panel foreground; Ctrl+C → stop semua service lalu keluar.
func console() error {
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- panel(stop) }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	close(stop)
	return <-done
}

// panel: first-run bootstrap (download, datadir, config) + Set/start semua
// service enabled + serve UI sampai stop di-close (console & service mode).
func panel(stop chan struct{}) error {
	root, err := stackRoot()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(root, "pawon-data")
	statePath := filepath.Join(dataDir, "pawon.json")
	logsDir := filepath.Join(dataDir, "logs")
	mainConf := filepath.Join(root, "nginx", "conf", "main.conf")

	trace := func(stage string) { fmt.Fprintln(os.Stderr, "pawon: "+stage) }
	st, err := state.Load(statePath)
	if err != nil {
		return err
	}
	trace("state dimuat")
	seed(&st)
	trace("seed")

	trace("download stack (first-run bisa lama)")
	if err := dl.Ensure(root, versions.Pinned); err != nil {
		return err
	}

	// nginx & mariadb ter-extract dengan top-dir versi → cari dinamis.
	nginxHome, err := svc.FindDir(filepath.Join(root, "bin", "nginx"), "nginx.exe")
	if err != nil {
		return err
	}
	trace("stack siap: " + filepath.Base(nginxHome))
	mariaHome, err := svc.FindDir(filepath.Join(root, "bin", "mariadb"), filepath.Join("bin", "mariadbd.exe"))
	if err != nil {
		return err
	}

	trace("mariadb home ok")
	datadir := filepath.Join(dataDir, "mysql")
	freshDB := false
	if _, err := os.Stat(datadir); os.IsNotExist(err) {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return err
		}
		if err := db.InitDatadir(mariaHome, datadir); err != nil {
			return err
		}
		freshDB = true
	}
	if st.DB.RootPassword == "" {
		st.DB.RootPassword = db.GenRootPassword()
	}
	if err := st.Save(statePath); err != nil {
		return err
	}
	trace("datadir siap (fresh=" + fmt.Sprint(freshDB) + ") + state tersimpan")

	if err := writeNginxConf(&st, root, nginxHome, mainConf, logsDir); err != nil {
		return err
	}
	trace("config nginx ditulis")
	// phpMyAdmin internal (pma.test): config + vhost + hosts (idempoten).
	if err := wirePMA(root, st.DB.RootPassword); err != nil {
		fmt.Fprintln(os.Stderr, "pawon: pma:", err)
	}

	sup := proc.New()
	sup.Set(proc.Spec{
		Name: "nginx",
		Exe:  filepath.Join(nginxHome, "nginx.exe"),
		Args: []string{"-p", prefix(nginxHome), "-c", filepath.ToSlash(mainConf)},
		Dir:  nginxHome,
	})
	sup.Set(proc.Spec{
		Name: "mariadb",
		Exe:  filepath.Join(mariaHome, "bin", "mariadbd.exe"),
		Args: []string{
			"--datadir=" + filepath.ToSlash(datadir),
			"--port=3306",
			// --log-error bukan --console: supervisor tidak menangkap stderr,
			// file log inilah yang dibaca log viewer UI.
			"--log-error=" + filepath.ToSlash(filepath.Join(logsDir, "mariadb.log")),
		},
		Dir: filepath.Join(mariaHome, "bin"),
	})
	for _, v := range st.PHPVersions {
		if !v.Enabled {
			continue
		}
		if err := ensurePhpIni(filepath.Join(root, "bin", "php", v.Version), logsDir, v.Version); err != nil {
			return err
		}
		for i, spec := range php.Instances(v) {
			spec.Name = fmt.Sprintf("php-%s-%d", v.Version, i+1)
			spec.Exe = filepath.Join(root, spec.Exe)
			spec.Dir = filepath.Join(root, spec.Dir)
			sup.Set(spec)
		}
	}

	api := cf.New(st.Cloudflare.APIToken)
	tun := &tunnel.Manager{
		St: &st, StatePath: statePath, API: api, Sup: sup,
		CloudflaredExe: filepath.Join(root, "bin", "cloudflared.exe"),
	}
	// Adapter CF hanya saat tunnel sudah di-setup; sebelum itu site lokal saja
	// (lihat komentar CFAdapter di server). Setup via UI → restart panel.
	var cfAdapter sites.Cloudflare
	if st.Cloudflare.TunnelID != "" {
		cfAdapter = server.NewCFAdapter(api, &st)
	}
	sm := &sites.Manager{
		St: &st, StatePath: statePath, StackRoot: root,
		Run: nginxRunner{exe: filepath.Join(nginxHome, "nginx.exe"), prefix: prefix(nginxHome), conf: mainConf},
		CF:  cfAdapter,
	}
	if sysroot := os.Getenv("SystemRoot"); sysroot != "" {
		sm.HostsPath = filepath.Join(sysroot, "System32", "drivers", "etc", "hosts")
	}

	// Start mariadb → php → nginx → cloudflared (kalau tunnel ada). Gagal satu
	// tidak menghentikan yang lain; status terlihat merah di Dashboard.
	for _, name := range startOrder(&st) {
		if err := sup.Start(name); err != nil {
			fmt.Fprintln(os.Stderr, "pawon: start", name, err)
		}
	}
	if err := tun.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "pawon: start cloudflared:", err)
	}
	if freshDB {
		if err := db.SetRootPassword(mariaHome, st.DB.RootPassword); err != nil {
			fmt.Fprintln(os.Stderr, "pawon: set root password:", err)
		}
	}

	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	deps := server.Deps{
		St: &st, StatePath: statePath, StackRoot: root,
		Sites: sm, Tunnel: tun, Sup: sup,
		MariadbDSN: func() string {
			return "root:" + st.DB.RootPassword + "@tcp(127.0.0.1:3306)/"
		},
		LogsDir: logsDir,
		Web:     webSub,
	}
	ln, err := net.Listen("tcp", panelAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", panelAddr, err)
	}
	srv := &http.Server{Handler: server.New(deps)}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	fmt.Println("Pawon panel: http://" + panelAddr)
	select {
	case err := <-serveErr:
		return err
	case <-stop:
		sup.StopAll()
		return srv.Close()
	}
}

// startOrder: nama service yang di-start, mariadb dulu (site butuh DB).
func startOrder(st *state.Config) []string {
	names := []string{"mariadb"}
	for _, v := range st.PHPVersions {
		if !v.Enabled {
			continue
		}
		for i := 1; i <= php.Workers; i++ {
			names = append(names, fmt.Sprintf("php-%s-%d", v.Version, i))
		}
	}
	return append(names, "nginx")
}

// seed: state masih kosong → default 1 pool PHP 8.4 + nginx/mariadb enabled
// (first-run harus langsung hijau tanpa edit manual pawon.json).
func seed(st *state.Config) {
	if len(st.PHPVersions) == 0 {
		st.PHPVersions = []state.PHPVersion{{Version: "8.4", PortBase: 9100, Enabled: true}}
	}
	if st.Services == nil {
		st.Services = map[string]state.ServiceCfg{"nginx": {Enabled: true}, "mariadb": {Enabled: true}}
	}
}

// writeNginxConf: main.conf + vhost per site dari state (sumber kebenaran
// yang sama dengan sites.Manager: <root>/nginx/conf{,/sites.d}), plus salin
// mime.types — include relatif di-resolve ke dir main.conf.
func writeNginxConf(st *state.Config, root, nginxHome, mainConf, logsDir string) error {
	confDir := filepath.Dir(mainConf)
	var ups []nginx.Upstream
	for _, v := range st.PHPVersions {
		if v.Enabled {
			ups = append(ups, nginx.Upstream{Name: php.PoolName(v.Version), PortBase: v.PortBase})
		}
	}
	var vhosts []nginx.Vhost
	for _, s := range st.Sites {
		vhosts = append(vhosts, nginx.Vhost{
			ServerNames: []string{s.Hostname, s.Subdomain + ".test"},
			Docroot:     s.Docroot,
			Pool:        php.PoolName(s.PHP),
			AccessLog:   filepath.ToSlash(filepath.Join(logsDir, "nginx", s.Hostname+"-access.log")),
			ErrorLog:    filepath.ToSlash(filepath.Join(logsDir, "nginx", s.Hostname+"-error.log")),
		})
	}
	if err := nginx.WriteAll(confDir, filepath.Join(confDir, "sites.d"), root, ups, vhosts); err != nil {
		return err
	}
	for _, f := range []string{"mime.types", "fastcgi_params"} {
		if err := copyIfMissing(filepath.Join(nginxHome, "conf", f), filepath.Join(confDir, f)); err != nil {
			return err
		}
	}
	return nil
}

// nginxRunner: Runner sites.Manager yang memvalidasi main.conf panel —
// nginx.Validate bawaan membaca conf/nginx.conf bawaan zip, bukan config kita.
type nginxRunner struct{ exe, prefix, conf string }

func (r nginxRunner) Validate() error { return runNginx(r.exe, r.prefix, "-t", "-c", r.conf) }
func (r nginxRunner) Reload() error   { return runNginx(r.exe, r.prefix, "-s", "reload", "-c", r.conf) }

func runNginx(exe, prefix string, args ...string) error {
	cmd := exec.Command(exe, "-p", prefix)
	cmd.Args = append(cmd.Args, args...)
	cmd.Dir = filepath.Dir(exe)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}

// prefix: arg -p nginx selalu eksplisit (dir exe) biar start/validate/reload
// dan default path (pid, temp) konsisten tanpa bergantung cwd.
func prefix(home string) string { return filepath.ToSlash(home) + "/" }

func copyIfMissing(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// stackRoot: dir pawon.exe — bin/, sites/, pawon-data/, nginx/ hidup di sini.
func stackRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Dir(exe))
}

// ensurePhpIni: zip PHP tidak membawa php.ini aktif — tulis minimal:
// extension Laravel + error log per versi (spec §7). Idempoten.
func ensurePhpIni(phpHome, logsDir, version string) error {
	ini := filepath.Join(phpHome, "php.ini")
	if _, err := os.Stat(ini); err == nil {
		return nil
	}
	ext := []string{"pdo_mysql", "mysqli", "mbstring", "gd", "zip", "intl", "sodium", "exif", "fileinfo", "curl"}
	var b strings.Builder
	fmt.Fprintln(&b, "; pawon: generated — panel tidak menimpa php.ini yang sudah ada")
	fmt.Fprintln(&b, `extension_dir = "ext"`)
	for _, e := range ext {
		fmt.Fprintf(&b, "extension=%s\n", e)
	}
	fmt.Fprintln(&b, "error_reporting = E_ALL")
	fmt.Fprintln(&b, "display_errors = Off")
	fmt.Fprintln(&b, "log_errors = On")
	fmt.Fprintf(&b, "error_log = %s\n", filepath.ToSlash(filepath.Join(logsDir, "php-"+version+".log")))
	return os.WriteFile(ini, []byte(b.String()), 0o644)
}

// wirePMA: phpMyAdmin sebagai site internal pma.test — config auth_type=config
// (kredensial root dari state), vhost, baris hosts. Semua idempoten.
func wirePMA(root, rootPW string) error {
	pmaHome, err := svc.FindDir(filepath.Join(root, "bin", "pma"), "index.php")
	if err != nil {
		return err
	}
	cfg := filepath.Join(pmaHome, "config.inc.php")
	if _, err := os.Stat(cfg); os.IsNotExist(err) {
		c := `<?php
$i = 0; $i++;
$cfg['Servers'][$i]['auth_type'] = 'config';
$cfg['Servers'][$i]['host'] = '127.0.0.1';
$cfg['Servers'][$i]['port'] = '3306';
$cfg['Servers'][$i]['user'] = 'root';
$cfg['Servers'][$i]['password'] = '` + rootPW + `';
$cfg['Servers'][$i]['AllowNoPassword'] = false;
`
		if err := os.WriteFile(cfg, []byte(c), 0o600); err != nil {
			return err
		}
	}
	confDir := filepath.Join(root, "nginx", "conf", "sites.d")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		return err
	}
	vhost := filepath.Join(confDir, "pma.test.conf")
	if _, err := os.Stat(vhost); os.IsNotExist(err) {
		logs := filepath.Join(root, "pawon-data", "logs", "nginx")
		v := nginx.RenderVhost(nginx.Vhost{
			ServerNames: []string{"pma.test"},
			Docroot:     pmaHome,
			Pool:        php.PoolName("8.4"),
			AccessLog:   filepath.ToSlash(filepath.Join(logs, "pma.test-access.log")),
			ErrorLog:    filepath.ToSlash(filepath.Join(logs, "pma.test-error.log")),
		})
		if err := os.WriteFile(vhost, []byte(v), 0o644); err != nil {
			return err
		}
	}
	if sysroot := os.Getenv("SystemRoot"); sysroot != "" {
		_ = hosts.Add(filepath.Join(sysroot, "System32", "drivers", "etc", "hosts"), "pma.test")
	}
	return nil
}
