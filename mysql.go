package coredns_mysql_extend

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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

var logger = clog.NewWithPlugin(pluginName)

type Mysql struct {
	Next          plugin.Handler
	dsn           string
	DB            *sql.DB
	DumpFile      string
	Cache         cache.Cache
	degradeCache  map[Record][]dns.RR
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

type PureRecord []map[string]string
type Record struct {
	ID       int
	ZoneID   int
	ZoneName string
	Name     string
	Type     string
	Value    string
	fqdn     string
	TTL      uint32
	rrString string
}

func MakeMysqlPlugin() *Mysql {
	return &Mysql{}
}

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

func (m *Mysql) OnStartup() error {
	m.Once.Do(func() {
		// Initialize database connection pool
		db, err := sql.Open("mysql", m.dsn)
		if err != nil {
			logger.Errorf("Failed to open database: %s", err)
		}

		m.DB = db
		logger.Debugf("mysql %#v", m)
		// Set default values
		if m.TTL == 0 {
			m.TTL = defaultTTL
		}

		if m.RetryInterval == 0 {
			m.RetryInterval = time.Second * 5
		}
		m.load()
		// Start retry loop
		go m.rePing()
		go m.reGetDomain()
	})

	err := m.createTables()
	if err != nil {
		logger.Error(err)
	}
	logger.Debugf("Load degrade data")
	// TODO
	return nil
}

func (m *Mysql) rePing() {
	for {
		if err := m.DB.Ping(); err != nil {
			logger.Errorf("Failed to ping database: %s", err)
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
			logger.Errorf("Failed to query domains: %s", err)
			time.Sleep(m.RetryInterval)
			continue
		}

		for rows.Next() {
			var domain Domain
			err := rows.Scan(&domain.ID, &domain.Name)
			if err != nil {
				logger.Error(err)
			}
			domainMap[domain.Name] = domain.ID
		}
		m.domainMap = domainMap
		logger.Debugf("domainmap %#v", domainMap)
		time.Sleep(time.Minute)
	}
}

func (m *Mysql) getDomainInfo(fqdn string) (int, string, string, error) {
	var (
		id    int
		host  string
		ok    bool
		zone  = fqdn
		items = strings.Split(zone, zoneSeparator)
	)

	// Only should case once but more. TODO
	for i := range items {
		zone = strings.Join(items[i:], zoneSeparator)
		id, ok = m.getZoneID(zone)
		host = strings.Join(items[:i], zoneSeparator)
		if ok {
			logger.Debugf("Query zone %s in zone cache", zone)
			return id, host, zone, nil
		}
	}

	return id, host, zone, fmt.Errorf("Domain not exist")
}

func (m *Mysql) getZoneID(zone string) (int, bool) {
	id, ok := m.domainMap[zone]
	return id, ok
}

func (m *Mysql) getBaseZone(fqdn string) string {
	if strings.Count(fqdn, zoneSeparator) > 1 {
		return strings.Join(strings.Split(fqdn, zoneSeparator)[1:], zoneSeparator)
	}
	return rootZone
}

func (m *Mysql) degradeQuery(record Record) ([]dns.RR, bool) {
	answers, ok := m.degradeCache[record]
	return answers, ok
}

func (m *Mysql) dump() {
	var pureRecord PureRecord
	pureRecord = make([]map[string]string, 0)
	for record := range m.degradeCache {
		pureRecord = append(pureRecord, map[string]string{
			fmt.Sprintf("%s:%s", record.fqdn, record.Type): record.rrString,
		})

	}

	content, err := json.Marshal(pureRecord)
	if err != nil {
		return
	}
	if err := os.WriteFile(m.DumpFile, content, safeMode); err != nil {
		logger.Error(err)
	}
}

func (m *Mysql) load() {
	m.degradeCache = make(map[Record][]dns.RR, 0)
	pureRecord := make([]map[string]string, 0)
	content, err := os.ReadFile(m.DumpFile)
	if err != nil {
		return
	}
	err = json.Unmarshal(content, &pureRecord)
	if err != nil {
		return
	}
	for _, rMap := range pureRecord {
		for queryKey, rrString := range rMap {
			queryKeySlice := strings.Split(queryKey, keySeparator)
			fqdn, qType := queryKeySlice[0], queryKeySlice[1]
			rr, err := dns.NewRR(rrString)
			record := Record{fqdn: fqdn, Type: qType}
			if err != nil {
				continue
			}
			m.degradeCache[record] = append(m.degradeCache[record], rr)
		}
	}
}

func (m *Mysql) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	var degradeAnswers []dns.RR
	var rrString string
	state := request.Request{W: w, Req: r}

	// Get domain name

	qName := state.Name()
	qType := state.Type()
	degradeRecord := Record{fqdn: qName, Type: qType}

	logger.Debugf("FQDN %s, DNS query type %s", qName, qType)

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

	// Query zone cache
	zoneID, host, zone, err := m.getDomainInfo(qName)
	logger.Debugf("ZoneID %d, host %s, zone %s", zoneID, host, zone)

	if err != nil {
		logger.Error(err)
		return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	var answers []dns.RR
	// full match
	records, err := m.getRecords(zoneID, host, zone, qType)
	logger.Debugf("domainID %d, host %s, qType %s, records %#v", zoneID, host, qType, records)
	if err != nil {
		logger.Errorf("Failed to get records for domain %s from database: %s", qName, err)
		logger.Debugf("Degrade records %#v", m.degradeCache)
		answers, ok := m.degradeQuery(degradeRecord)
		if !ok {
			return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
		}
		degradeAnswers = answers
		goto DegradeEnterpoint
	}

	// try query CNAME type of record
	if len(records) == zero {
		cnameRecords, err := m.getRecords(zoneID, host, zone, cnameQtype)
		logger.Debugf("domainID %d, host %s, qType %s, records %#v", zoneID, host, cnameQtype, records)
		if err != nil {
			logger.Errorf("Failed to get records for domain %s from database: %s", qName, err)
			return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
		}
		for _, cnameRecord := range cnameRecords {
			cnameZoneID, cnameHost, cnameZone, err := m.getDomainInfo(cnameRecord.Value)
			logger.Debugf("ZoneID %d, host %s, zone %s", cnameZoneID, cnameHost, cnameZone)

			if err != nil {
				logger.Error(err)
				return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
			}

			rrString = fmt.Sprintf("%s %d IN %s %s", qName, cnameRecord.TTL, cnameRecord.Type, cnameRecord.Value)
			degradeRecord.rrString = rrString
			rr, err := dns.NewRR(rrString)
			if err != nil {
				logger.Errorf("Failed to create DNS record: %s", err)
				continue
			}
			answers = append(answers, rr)

			cname2Records, err := m.getRecords(cnameZoneID, cnameHost, cnameZone, qType)
			logger.Debugf("domainID %d, host %s, qType %s, records %#v", cnameZoneID, cnameHost, qType, records)

			if err != nil {
				logger.Errorf("Failed to get domain %s from database: %s", cnameHost+zoneSeparator+cnameZone, err)
				return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
			}
			for _, cname2Record := range cname2Records {
				rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", cname2Record.Name, cname2Record.TTL, cname2Record.Type, cname2Record.Value))
				if err != nil {
					logger.Errorf("Failed to create DNS record: %s", err)
					continue
				}
				answers = append(answers, rr)
			}
		}
	}

	// Process records
	for _, record := range records {
		rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", record.Name, record.TTL, record.Type, record.Value))
		if err != nil {
			logger.Errorf("Failed to create DNS record: %s", err)
			continue
		}
		answers = append(answers, rr)
	}

	// Handle wildcard domains
	if len(answers) == zero && strings.Count(qName, zoneSeparator) > 1 {
		baseZone := m.getBaseZone(qName)
		domainID, ok := m.getZoneID(baseZone)
		wildcardName := wildcard + zoneSeparator + baseZone
		if !ok {
			logger.Errorf("Failed to get domain %s from database: %s", qName, err)
			return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
		}
		records, err := m.getRecords(domainID, wildcard, zone, qType)
		if err != nil {
			logger.Errorf("Failed to get records for domain %s from database: %s", wildcardName, err)
			return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
		}

		for _, record := range records {
			rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", wildcardName, record.TTL, record.Type, record.Value))
			if err != nil {
				logger.Errorf("Failed to create DNS record: %s", err)
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
DegradeEnterpoint:
	answers = append(answers, degradeAnswers...)
	// Return result
	if len(answers) > 0 {
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.Answer = answers
		w.WriteMsg(msg)
		m.degradeCache[degradeRecord] = answers
		logger.Debugf("Add degrade record %#v", degradeRecord)
		return dns.RcodeSuccess, nil
	}

	return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
}

func (m *Mysql) getRecords(domainID int, host, zone, qtype string) ([]Record, error) {
	var records []Record
	baseQuerySql := `SELECT id, domain_id, name, type, value, ttl FROM ` + m.RecordsTable + ` WHERE domain_id=? and name=? and type=?`

	rows, err := m.DB.Query(baseQuerySql, domainID, host, qtype)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var record Record
		err := rows.Scan(&record.ID, &record.ZoneID, &record.Name, &record.Type, &record.Value, &record.TTL)
		if err != nil {
			return nil, err
		}
		record.ZoneName = zone
		logger.Debugf("record %#v", record)
		records = append(records, record)
	}
	return records, nil
}

func (m *Mysql) OnShutdown() error {
	for k, v := range m.degradeCache {
		logger.Debugf("record %#v, answers %#v", k, v)
	}
	if m.DB != nil {
		m.DB.Close()
	}
	m.dump()
	return nil
}

// func (m *Mysql) Debug() {
// 	logger.Debugf("[DEBUG] MySQL plugin configuration: %+v", m)
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
