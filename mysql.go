package coredns_mysql_extend

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/pkg/cache"

	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"
	_ "github.com/go-sql-driver/mysql"
	"github.com/miekg/dns"
)

var dnsLogger = clog.NewWithPlugin(pluginName)

type Mysql struct {
	Next          plugin.Handler
	dsn           string
	DB            *sql.DB
	Cache         cache.Cache
	domainMap     map[string]int
	TTL           uint32
	RetryInterval time.Duration
	DomainsTable  string
	RecordsTable  string
	LogEnabled    bool
	Once          sync.Once
}

type Domain struct {
	ID   int
	Name string
}

type Record struct {
	ID       int
	DomainID int
	Name     string
	Type     string
	Value    string
	TTL      uint32
}

func MakeMysqlPlugin() *Mysql {
	return &Mysql{}
}

func (m *Mysql) Name() string {
	return pluginName
}

func (m *Mysql) ParseConfig(c *caddy.Controller) error {
	for c.Next() {
		log.Printf("%#v", c)
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
			// case "cache":
			// 	if !c.Args(&m.TTL) {
			// 		return c.ArgErr()
			// 	}
			// case "retry_interval":
			// 	if !c.Args(&m.RetryInterval) {
			// 		return c.ArgErr()
			// 	}
			case "log_enabled":
				m.LogEnabled = true
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

func (m *Mysql) OnStartup() error {
	m.Once.Do(func() {
		// Initialize database connection pool
		db, err := sql.Open("mysql", m.dsn)
		if err != nil {
			dnsLogger.Fatalf("Failed to connect to database: %s", err)
		}

		m.DB = db
		// Set default values
		if m.TTL == 0 {
			m.TTL = defaultTTL
		}

		if m.RetryInterval == 0 {
			m.RetryInterval = time.Second * 5
		}

		// Start retry loop
		go m.rePing()
		go m.reGetDomain()
	})

	err := m.createTables()
	if err != nil {
		return err
	}
	return nil
}

func (m *Mysql) rePing() {
	for {
		if err := m.DB.Ping(); err != nil {
			dnsLogger.Debugf("[ERROR] Failed to ping database: %s", err)
			time.Sleep(m.RetryInterval)
			continue
		}
		time.Sleep(time.Minute)
	}
}

func (m *Mysql) reGetDomain() {
	var domainMap = make(map[string]int, 0)
	for {
		rows, err := m.DB.Query("SELECT id, name FROM " + m.DomainsTable)
		if err != nil {
			dnsLogger.Error(err)
		}

		for rows.Next() {
			var domain Domain
			err := rows.Scan(&domain.ID, &domain.Name)
			if err != nil {
				dnsLogger.Error(err)
			}
			domainMap[domain.Name] = domain.ID
		}
		m.domainMap = domainMap
		dnsLogger.Infof("domainmap %#v", domainMap)
		time.Sleep(time.Minute)
	}
}

func (m *Mysql) getDomainInfo(fqdn string) (int, string, error) {
	var (
		id    int
		host  string
		ok    bool
		zone  = fqdn
		items = strings.Split(zone, zoneSeparator)
	)

	for i := range items {
		zone = strings.Join(items[i:], zoneSeparator)
		dnsLogger.Infof("zone %#v", zone)
		id, ok = m.getDomainID(zone)
		host = strings.Join(items[:i], zoneSeparator)
		if ok {
			return id, host, nil
		}
	}

	return id, host, fmt.Errorf("Domain not exist")
}

func (m *Mysql) getDomainID(zone string) (int, bool) {
	id, ok := m.domainMap[zone]
	return id, ok
}

func (m *Mysql) getBaseZone(fqdn string) string {
	if strings.Count(fqdn, zoneSeparator) > 1 {
		return strings.Join(strings.Split(fqdn, zoneSeparator)[1:], zoneSeparator)
	}
	return rootZone
}

func (m *Mysql) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	// Get domain name

	qName := state.Name()
	qType := state.Type()

	dnsLogger.Infof("qname %s, qtype %s", qName, qType)

	// if !strings.HasSuffix(domainName, ".") {
	// 	domainName += "."
	// }

	// Check cache first
	// if m.Cache != nil {
	// 	key := plugin.EncodeCacheKey(domainName, dns.TypeA)
	// 	if a, ok := m.Cache.Get(key); ok {
	// 		msg := new(dns.Msg)
	// 		msg.SetReply(r)
	// 		msg.Answer = []dns.RR{a.(dns.RR)}
	// 		w.WriteMsg(msg)
	// 		return dns.RcodeSuccess, nil
	// 	}
	// }

	// Query database
	domainID, host, err := m.getDomainInfo(qName)

	if err != nil {
		dnsLogger.Debugf("[ERROR] Failed to get domain %s from database: %s", qName, err)
		return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	records, err := m.getRecords(domainID, host, qType)
	dnsLogger.Infof("records %#v", records)
	if err != nil {
		dnsLogger.Debugf("[ERROR] Failed to get records for domain %s from database: %s", qName, err)
		return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	// Process records
	var answers []dns.RR
	for _, record := range records {
		rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", record.Name, record.TTL, record.Type, record.Value))
		if err != nil {
			dnsLogger.Debugf("[ERROR] Failed to create DNS record: %s", err)
			continue
		}
		answers = append(answers, rr)
	}

	// Handle wildcard domains
	if len(answers) == 0 && strings.Count(qName, zoneSeparator) > 1 {
		baseZone := m.getBaseZone(qName)
		domainID, ok := m.getDomainID(baseZone)
		wildcardName := wildcard + zoneSeparator + baseZone
		if !ok {
			dnsLogger.Infof("[ERROR] Failed to get domain %s from database: %s", qName, err)
			return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
		}
		records, err := m.getRecords(domainID, wildcard, qType)
		if err != nil {
			dnsLogger.Infof("[ERROR] Failed to get records for domain %s from database: %s", wildcardName, err)
			return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
		}

		for _, record := range records {
			rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", wildcardName, record.TTL, record.Type, record.Value))
			if err != nil {
				dnsLogger.Infof("[ERROR] Failed to create DNS record: %s", err)
				continue
			}
			answers = append(answers, rr)
		}
	}

	// Cache result
	// if m.Cache != nil && len(answers) > 0 {
	// 	key := plugin.EncodeCacheKey(domainName, dns.TypeA)
	// 	m.Cache.Set(key, answers[0], time.Duration(m.TTL)*time.Second)
	// }

	// Return result
	if len(answers) > 0 {
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.Answer = answers
		w.WriteMsg(msg)
		return dns.RcodeSuccess, nil
	}

	return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
}

func (m *Mysql) getRecords(domainID int, host string, qtype string) ([]Record, error) {
	var records []Record
	baseQuerySql := `SELECT id, domain_id, name, type, value, ttl FROM ` + m.RecordsTable + ` WHERE domain_id=? and name=? and type=?`
	dnsLogger.Infof("Baseurl %v, doamin_id %v, host %v, qtype %v", baseQuerySql, domainID, host, qtype)
	rows, err := m.DB.Query(baseQuerySql, domainID, host, qtype)
	dnsLogger.Infof("rows %#v, err %v", rows, err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var record Record
		err := rows.Scan(&record.ID, &record.DomainID, &record.Name, &record.Type, &record.Value, &record.TTL)
		if err != nil {
			return nil, err
		}
		dnsLogger.Infof("record %#v", record)
		records = append(records, record)
	}
	return records, nil
}

func (m *Mysql) OnShutdown() error {
	m.DB.Close()
	return nil
}

// func (m *Mysql) Debug() {
// 	dnsLogger.Debugf("[DEBUG] MySQL plugin configuration: %+v", m)
// }

// func (m *Mysql) Metrics() []plugin.Metric {
// 	return nil
// }

// func MysqlPlugin(c context.Context, dsn string, domainsTable string, recordsTable string, ttl uint32, retryInterval time.Duration, logEnabled bool) (plugin.Plugin, error) {
// 	return &Mysql{
// 		DB:            nil,
// 		Cache:         nil,
// 		TTL:           ttl,
// 		RetryInterval: retryInterval,
// 		DomainsTable:  domainsTable,
// 		RecordsTable:  recordsTable,
// 		LogEnabled:    logEnabled,
// 	}, nil
// }
