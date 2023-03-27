package coredns_mysql_extend

import (
	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
)

func init() {
	plugin.Register(pluginName, setup)
}

// setup is the function that gets called when the config parser see the token "example". Setup is responsible
// for parsing any extra options the example plugin may have. The first token this function sees is "example".
func setup(c *caddy.Controller) error {
	// Get new mysql plugin
	mysql := MakeMysqlPlugin()

	// Parse configuration
	mysql.parseConfig(c)

	// Init global var
	zoneQuerySQL = `SELECT id, zone_name FROM ` + mysql.zonesTable
	recordQuerySQL = `SELECT id, zone_id, hostname, type, data, ttl, online FROM ` + mysql.recordsTable + ` WHERE domain_id=? and name=? and type=?`

	// Exec options when start up
	c.OnStartup(mysql.onStartup)

	// Exec options when shut down
	c.OnShutdown(mysql.onShutdown)

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		mysql.Next = next
		return mysql
	})

	// All OK, return a nil error.
	return nil
}
