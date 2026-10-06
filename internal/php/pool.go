package php

import (
	"os"
	"strconv"
	"strings"

	"pawon/internal/proc"
	"pawon/internal/state"
)

// Workers is the fixed php-cgi worker count per PHP version.
const Workers = 4

// PoolName maps a PHP version ("8.4") to its nginx upstream pool name ("php_84_pool").
func PoolName(version string) string {
	return "php_" + strings.ReplaceAll(version, ".", "") + "_pool"
}

// Instances builds one php-cgi spec per worker, each bound to portBase+i.
func Instances(v state.PHPVersion) []proc.Spec {
	specs := make([]proc.Spec, Workers)
	for i := range specs {
		specs[i] = proc.Spec{
			Name: "php-" + v.Version,
			Exe:  "bin/php/" + v.Version + "/php-cgi.exe",
			Args: []string{"-b", "127.0.0.1:" + strconv.Itoa(v.PortBase+i)},
			// cmd.Env menggantikan seluruh environment; tanpa TMP/TEMP
			// php-cgi fallback ke C:\WINDOWS (tak writable) → upload
			// gagal UPLOAD_ERR_NO_TMP_DIR.
			Env: []string{
				"PHP_FCGI_MAX_REQUESTS=500",
				"SystemRoot=" + os.Getenv("SystemRoot"),
				"TMP=" + os.TempDir(),
				"TEMP=" + os.TempDir(),
			},
			Dir:  "bin/php/" + v.Version,
			Port: v.PortBase + i,
		}
	}
	return specs
}
