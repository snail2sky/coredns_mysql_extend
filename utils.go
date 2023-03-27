package coredns_mysql_extend

import (
	"fmt"
	"strings"

	"github.com/miekg/dns"
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
	dnsRecordInfo, ok := m.degradeCache[record]
	return dnsRecordInfo.response, ok
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
