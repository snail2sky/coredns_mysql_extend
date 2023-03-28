package coredns_mysql_extend

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

func MakeMysqlPlugin() *Mysql {
	return &Mysql{}
}

func MakeMessage(r *dns.Msg, answers []dns.RR) *dns.Msg {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Answer = answers
	return msg
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
		if host == "" {
			host = zoneSelf
		}
		if ok {
			logger.Debugf("Query zone %s in zone cache", zone)
			// TODO zone_find{'status'='success'}
			return id, host, zone, nil
		}
	}
	// TODO zone_find{'status'='fail'}
	return id, host, zone, fmt.Errorf("domain %s not exist", fqdn)
}

func (m *Mysql) getZoneID(zone string) (int, bool) {
	id, ok := m.zoneMap[zone]
	return id, ok
}

func (m *Mysql) getBaseZone(fqdn string) string {
	if strings.Count(fqdn, zoneSeparator) > 1 {
		return strings.Join(strings.Split(fqdn, zoneSeparator)[1:], zoneSeparator)
	}
	return rootZone
}

func (m *Mysql) degradeQuery(record record) ([]dns.RR, bool) {
	dnsRecordInfo, ok := m.degradeCache[record]
	if !ok {
		degradeCacheCount.With(prometheus.Labels{"option": "query", "status": "fail", "fqdn": record.fqdn, "qtype": record.qType}).Inc()
		// TODO degrade_cache{option='query', status='fail', fqdn='record.fqdn', qtype='record.qType'}
	} else {
		degradeCacheCount.With(prometheus.Labels{"option": "query", "status": "success", "fqdn": record.fqdn, "qtype": record.qType}).Inc()
		// TODO degrade_cache{option='query', status='success', fqdn='record.fqdn', qtype='record.qType'}
	}
	return dnsRecordInfo.response, ok
}

func (m *Mysql) degradeWrite(record record, dnsRecordInfo dnsRecordInfo) {
	m.degradeCache[record] = dnsRecordInfo
}

func (m *Mysql) getRecords(domainID int, host, zone, qtype string) ([]record, error) {
	var records []record

	rows := m.DB.QueryRow(m.queryRecordSQL, domainID, host, qtype)

	for {
		var record record
		err := rows.Scan(&record.id, &record.zoneID, &record.name, &record.qType, &record.data, &record.ttl)
		if err == sql.ErrNoRows {
			// TODO query_db{status='success'}
			return records, nil
		}
		if err != nil {
			// TODO query_db{status='fail'}
			return nil, err
		}
		record.zoneName = zone
		logger.Debugf("record %#v", record)
		records = append(records, record)
	}
}

func (m *Mysql) makeAnswer(rrString string) (dns.RR, error) {
	rr, err := dns.NewRR(rrString)
	if err != nil {
		// TODO make_answer{status='fail'}
		logger.Errorf("Failed to create DNS record: %s", err)
	}
	// TODO make_answer{status='success'}
	return rr, nil
}
