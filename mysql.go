package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"
	_ "github.com/go-sql-driver/mysql"
	"github.com/miekg/dns"
)

const (
	defaultTTL = 300
	pluginName = "mysql"
)

var log = clog.NewWithPlugin(pluginName)

type Mysql struct {
	Next          plugin.Handler
	DB            *sql.DB
	Cache         plugin.Cache
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

func MakeMysqlPlugin() plugin.Plugin {
	return &Mysql{}
}

func (m *Mysql) Name() string {
	return pluginName
}

func (m *Mysql) ParseConfig(c *caddy.Controller) error {
	for c.Next() {
		log.Debugf("%#v", c)
		if !c.Args(&m.DomainsTable, &m.RecordsTable) {
			return c.ArgErr()
		}

		for c.NextBlock() {
			switch c.Val() {
			case "cache":
				if !c.Args(&m.TTL) {
					return c.ArgErr()
				}
			case "retry_interval":
				if !c.Args(&m.RetryInterval) {
					return c.ArgErr()
				}
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
		db, err := sql.Open("mysql", "user:password@tcp(localhost:3306)/dns")
		if err != nil {
			log.Fatalf("[FATAL] Failed to connect to database: %s", err)
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
		go m.retryLoop()
	})
	return nil
}

func (m *Mysql) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	// Get domain name
	domainName := state.Name()
	if !strings.HasSuffix(domainName, ".") {
		domainName += "."
	}

	// Check cache first
	if m.Cache != nil {
		key := plugin.EncodeCacheKey(domainName, dns.TypeA)
		if a, ok := m.Cache.Get(key); ok {
			msg := new(dns.Msg)
			msg.SetReply(r)
			msg.Answer = []dns.RR{a.(dns.RR)}
			w.WriteMsg(msg)
			return dns.RcodeSuccess, nil
		}
	}

	// Query database
	domain, err := m.getDomain(domainName)
	if err != nil {
		log.Printf("[ERROR] Failed to get domain %s from database: %s", domainName, err)
		return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	if domain == nil {
		return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	records, err := m.getRecords(domain.ID)
	if err != nil {
		log.Printf("[ERROR] Failed to get records for domain %s from database: %s", domainName, err)
		return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	// Process records
	var answers []dns.RR
	for _, record := range records {
		if record.Type == "A" || record.Type == "AAAA" || record.Type == "CNAME" || record.Type == "SRV" || record.Type == "SOA" || record.Type == "NS" || record.Type == "PTR" {
			rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", record.Name, record.TTL, record.Type, record.Value))
			if err != nil {
				log.Printf("[ERROR] Failed to create DNS record: %s", err)
				continue
			}
			answers = append(answers, rr)
		}
	}

	// Handle wildcard domains
	if len(answers) == 0 && strings.Count(domainName, ".") > 1 {
		wildcardName := "*." + strings.Join(strings.Split(domainName, ".")[1:], ".")
		records, err := m.getRecords(domain.ID)
		if err != nil {
			log.Printf("[ERROR] Failed to get records for domain %s from database: %s", wildcardName, err)
			return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
		}

		for _, record := range records {
			if record.Type == "A" || record.Type == "AAAA" || record.Type == "CNAME" || record.Type == "SRV" || record.Type == "SOA" || record.Type == "NS" || record.Type == "PTR" {
				rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", wildcardName, record.TTL, record.Type, record.Value))
				if err != nil {
					log.Printf("[ERROR] Failed to create DNS record: %s", err)
					continue
				}
				answers = append(answers, rr)
			}
		}
	}

	// Cache result
	if m.Cache != nil && len(answers) > 0 {
		key := plugin.EncodeCacheKey(domainName, dns.TypeA)
		m.Cache.Set(key, answers[0], time.Duration(m.TTL)*time.Second)
	}

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

func (m *Mysql) getDomain(name string) (*Domain, error) {
	var domain Domain
	err := m.DB.QueryRow("SELECT id, name FROM "+m.DomainsTable+" WHERE name=?", name).Scan(&domain.ID, &domain.Name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &domain, nil
}

func (m *Mysql) getRecords(domainID int) ([]Record, error) {
	var records []Record
	rows, err := m.DB.Query("SELECT id, domain_id, name, type, value, ttl FROM "+m.RecordsTable+" WHERE domain_id=?", domainID)
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
		records = append(records, record)
	}
	return records, nil
}

func (m *Mysql) retryLoop() {
	for {
		if err := m.DB.Ping(); err != nil {
			log.Printf("[ERROR] Failed to ping database: %s", err)
			time.Sleep(m.RetryInterval)
			continue
		}
		time.Sleep(time.Minute)
	}
}

func (m *Mysql) OnShutdown() error {
	m.DB.Close()
	return nil
}

func (m *Mysql) Debug() {
	log.Printf("[DEBUG] MySQL plugin configuration: %+v", m)
}

func (m *Mysql) Metrics() []plugin.Metric {
	return nil
}

func MysqlPlugin(c context.Context, dsn string, domainsTable string, recordsTable string, ttl uint32, retryInterval time.Duration, logEnabled bool) (plugin.Plugin, error) {
	return &Mysql{
		DB:            nil,
		Cache:         nil,
		TTL:           ttl,
		RetryInterval: retryInterval,
		DomainsTable:  domainsTable,
		RecordsTable:  recordsTable,
		LogEnabled:    logEnabled,
	}, nil
}
