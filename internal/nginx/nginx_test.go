package nginx

import (
	"strings"
	"testing"
)

func TestRenderVhostHasPoolAndLogs(t *testing.T) {
	out := RenderVhost(Vhost{
		ServerNames: []string{"app.example.com", "app.test"},
		Docroot:     "C:/pawon/sites/app/public",
		Pool:        "php_84_pool",
		AccessLog:   "C:/pawon/pawon-data/logs/nginx/app.example.com-access.log",
		ErrorLog:    "C:/pawon/pawon-data/logs/nginx/app.example.com-error.log",
	})
	for _, want := range []string{
		"server_name app.example.com app.test;",
		`root "C:/pawon/sites/app/public";`,
		"fastcgi_pass php_84_pool;",
		"try_files $uri $uri/ /index.php?$query_string;",
		"try_files $fastcgi_script_name =404;",
		"access_log",
		"error_log",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("vhost missing %q:\n%s", want, out)
		}
	}
}

func TestRenderMainUpstreamFourServers(t *testing.T) {
	out := RenderMain([]Upstream{{Name: "php_84_pool", PortBase: 9100}}, "C:/pawon")
	for _, want := range []string{
		"upstream php_84_pool {",
		"server 127.0.0.1:9100;", "server 127.0.0.1:9101;",
		"server 127.0.0.1:9102;", "server 127.0.0.1:9103;",
		"include sites.d/*.conf;",
		"error_log",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("main missing %q:\n%s", want, out)
		}
	}
}
