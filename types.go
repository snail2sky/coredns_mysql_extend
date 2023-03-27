package coredns_mysql_extend

import (
	"database/sql"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/cache"
	"github.com/miekg/dns"
)

type Mysql struct {
	Next          plugin.Handler
	dsn           string
	DB            *sql.DB
	DumpFile      string
	Cache         cache.Cache
	degradeCache  map[Record]DnsRecordInfo
	domainMap     map[string]int
	TTL           uint32
	RetryInterval time.Duration
	DomainsTable  string
	RecordsTable  string
	LogEnabled    bool
	Once          sync.Once
}

type DnsRecordInfo struct {
	response  []dns.RR
	rrStrings []string
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
}
