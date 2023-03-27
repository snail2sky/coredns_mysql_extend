package coredns_mysql_extend

import (
	"database/sql"
	"sync"
	"time"

	"github.com/coredns/coredns/plugin"
	"github.com/miekg/dns"
)

type Mysql struct {
	mysqlConfig

	degradeCache map[record]dnsRecordInfo
	zoneMap      map[string]int

	Next plugin.Handler
	DB   *sql.DB

	Once sync.Once
}

type pureRecord map[string][]string

type mysqlConfig struct {
	dsn          string
	dumpFile     string
	ttl          uint32
	zonesTable   string
	recordsTable string

	maxIdleConns    int
	maxOpenConns    int
	connMaxIdleTime time.Duration
	connMaxLifetime time.Duration

	failHeartbeatTime    time.Duration
	successHeartbeatTime time.Duration
}

type dnsRecordInfo struct {
	response  []dns.RR
	rrStrings []string
}

type zoneRecord struct {
	id   int
	name string
}

type record struct {
	ID       int
	ZoneID   int
	ZoneName string
	Name     string
	Type     string
	Value    string
	fqdn     string
	TTL      uint32
}
