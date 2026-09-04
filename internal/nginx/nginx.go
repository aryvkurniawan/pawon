// Package nginx merender config nginx (main.conf + vhost per site) dan
// menjalankan validate/reload.
package nginx

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

type Vhost struct {
	ServerNames         []string // [hostname, <sub>.test]
	Docroot             string
	Pool                string // "php_84_pool"
	AccessLog, ErrorLog string // path absolut di pawon-data/logs/nginx/
}

type Upstream struct {
	Name     string
	PortBase int // 4 server per upstream: portBase..+3
}

// RenderVhost merender satu server block sesuai §7 spec.
func RenderVhost(v Vhost) string {
	return fmt.Sprintf(`server {
    listen 80;
    server_name %s;
    root "%s";
    access_log "%s";
    error_log  "%s";
    index index.php index.html;
    location / { try_files $uri $uri/ /index.php?$query_string; }
    location ~ \.php$ {
        try_files $fastcgi_script_name =404;
        include fastcgi_params;
        fastcgi_pass %s;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
    }
}
`, strings.Join(v.ServerNames, " "), v.Docroot, v.AccessLog, v.ErrorLog, v.Pool)
}

// RenderMain merender nginx.conf: upstream pool per versi PHP + include vhost.
func RenderMain(ups []Upstream, stackRoot string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "worker_processes  1;\n\nerror_log  \"%s\";\n\n", path.Join(stackRoot, "pawon-data", "logs", "nginx-error.log"))
	b.WriteString("events {\n    worker_connections  1024;\n}\n\nhttp {\n    include       mime.types;\n    default_type  application/octet-stream;\n    sendfile      on;\n\n")
	for _, u := range ups {
		fmt.Fprintf(&b, "    upstream %s {\n", u.Name)
		for p := u.PortBase; p < u.PortBase+4; p++ {
			fmt.Fprintf(&b, "        server 127.0.0.1:%d;\n", p)
		}
		b.WriteString("    }\n\n")
	}
	b.WriteString("    include sites.d/*.conf;\n}\n")
	return b.String()
}

// WriteAll membuat dir logs/nginx + sites.d, lalu menulis main.conf dan
// satu .conf per vhost (nama file <hostname>.conf).
// Rollback: pemanggil menjalankan Validate setelah menulis; gagal → hapus
// .conf yang baru ditulis lalu Reload config lama.
func WriteAll(nginxDir, sitesDir, stackRoot string, ups []Upstream, vhosts []Vhost) error {
	if err := os.MkdirAll(filepath.Join(stackRoot, "pawon-data", "logs", "nginx"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(nginxDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(sitesDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(nginxDir, "main.conf"), []byte(RenderMain(ups, stackRoot)), 0o644); err != nil {
		return err
	}
	for _, v := range vhosts {
		if err := os.WriteFile(filepath.Join(sitesDir, v.ServerNames[0]+".conf"), []byte(RenderVhost(v)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func runNginx(nginxExe string, args ...string) error {
	cmd := exec.Command(nginxExe, args...)
	cmd.Dir = filepath.Dir(nginxExe)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Validate menjalankan `nginx -t`; exit 0 = ok.
func Validate(nginxExe string) error { return runNginx(nginxExe, "-t") }

// Reload menjalankan `nginx -s reload`.
func Reload(nginxExe string) error { return runNginx(nginxExe, "-s", "reload") }
