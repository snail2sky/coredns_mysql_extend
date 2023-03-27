package coredns_mysql_extend

import "github.com/coredns/caddy"

func (m *Mysql) Name() string {
	return pluginName
}
func (m *Mysql) ParseConfig(c *caddy.Controller) error {
	for c.Next() {
		for c.NextBlock() {
			switch c.Val() {
			case "dsn":
				if !c.NextArg() {
					return c.ArgErr()
				}
				m.dsn = c.Val()
			case "domains_table":
				if !c.NextArg() {
					return c.ArgErr()
				}
				m.DomainsTable = c.Val()
			case "records_table":
				if !c.NextArg() {
					return c.ArgErr()
				}
				m.RecordsTable = c.Val()
			case "dumpfile":
				if !c.NextArg() {
					return c.ArgErr()
				}
				m.DumpFile = c.Val()
			// case "cache":
			// 	if !c.Args(&m.TTL) {
			// 		return c.ArgErr()
			// 	}
			// case "retry_interval":
			// 	if !c.Args(&m.RetryInterval) {
			// 		return c.ArgErr()
			// 	}
			default:
				return c.Errf("unknown property '%s'", c.Val())
			}
		}
	}
	return nil
}

func (m *Mysql) createTables() error {
	_, err := m.DB.Exec(`
        CREATE TABLE IF NOT EXISTS ` + m.DomainsTable + ` (
            id INT NOT NULL AUTO_INCREMENT,
            name VARCHAR(255) NOT NULL,
            PRIMARY KEY (id),
            UNIQUE KEY (name)
        );
    `)
	if err != nil {
		return err
	}

	_, err = m.DB.Exec(`
        CREATE TABLE IF NOT EXISTS ` + m.RecordsTable + ` (
            id INT NOT NULL AUTO_INCREMENT,
            domain_id INT NOT NULL,
            name VARCHAR(255) NOT NULL,
            type VARCHAR(10) NOT NULL,
            value VARCHAR(255) NOT NULL,
            ttl INT NOT NULL,
            PRIMARY KEY (id),
            FOREIGN KEY (domain_id) REFERENCES ` + m.DomainsTable + `(id)
        );
    `)
	if err != nil {
		return err
	}

	return nil
}
