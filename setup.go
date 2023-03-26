package coredns_mysql_extend

import (
	"log"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
)

func init() {
	plugin.Register(pluginName, setup)
}

// setup is the function that gets called when the config parser see the token "example". Setup is responsible
// for parsing any extra options the example plugin may have. The first token this function sees is "example".
func setup(c *caddy.Controller) error {
	log.SetFlags(log.Lshortfile | log.LstdFlags)
	clog.D.Set()
	mysql := MakeMysqlPlugin()
	mysql.ParseConfig(c)

	c.OnStartup(mysql.OnStartup)
	c.OnShutdown(mysql.OnShutdown)

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		mysql.Next = next
		return mysql
	})

	// All OK, return a nil error.
	return nil
}
