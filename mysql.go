package coredns_mysql_extend

import (
	"context"
	"fmt"
	"strings"

	"github.com/coredns/coredns/plugin"

	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"
	_ "github.com/go-sql-driver/mysql"
	"github.com/miekg/dns"
)

var logger = clog.NewWithPlugin(pluginName)

func (m *Mysql) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	answers := make([]dns.RR, 0)
	var records []Record
	var rrStrings = make([]string, 0)
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
		goto DegradeEntrypoint

		// logger.Error(err)
		// return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
	}

	// Query DB, full match
	records, err = m.getRecords(zoneID, host, zone, qType)
	logger.Debugf("domainID %d, host %s, qType %s, records %#v", zoneID, host, qType, records)
	if err != nil {
		logger.Errorf("Failed to get records for domain %s from database: %s", qName, err)
		logger.Debugf("Degrade records %#v", m.degradeCache)
		goto DegradeEntrypoint
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

			rrString := fmt.Sprintf("%s %d IN %s %s", qName, cnameRecord.TTL, cnameRecord.Type, cnameRecord.Value)
			rrStrings = append(rrStrings, rrString)
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
				rr, err := dns.NewRR(fmt.Sprintf("%s %d IN %s %s", cname2Record.Name+zoneSeparator+cname2Record.ZoneName, cname2Record.TTL, cname2Record.Type, cname2Record.Value))
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
		rrString := fmt.Sprintf("%s %d IN %s %s", record.Name, record.TTL, record.Type, record.Value)
		rr, err := dns.NewRR(rrString)
		rrStrings = append(rrStrings, rrString)
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
			rrString := fmt.Sprintf("%s %d IN %s %s", wildcardName, record.TTL, record.Type, record.Value)
			rr, err := dns.NewRR(rrString)
			rrStrings = append(rrStrings, rrString)
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

	// Return result
	if len(answers) > 0 {
		msg := MakeMessage(r, answers)
		w.WriteMsg(msg)
		dnsRecordInfo := DnsRecordInfo{rrStrings: rrStrings, response: answers}
		m.degradeCache[degradeRecord] = dnsRecordInfo
		logger.Debugf("Add degrade record %#v, dnsRecordInfo %#v", degradeRecord, dnsRecordInfo)
		return dns.RcodeSuccess, nil
	}
DegradeEntrypoint:
	if answers, ok := m.degradeQuery(degradeRecord); ok {
		msg := MakeMessage(r, answers)
		w.WriteMsg(msg)
		logger.Debugf("Add degrade record %#v", degradeRecord)
		w.WriteMsg(msg)
		return dns.RcodeSuccess, nil
	}

	return plugin.NextOrFailure(m.Name(), m.Next, ctx, w, r)
}

// func (m *Mysql) Debug() {
// 	logger.Debugf("[DEBUG] MySQL plugin configuration: %+v", m)
// }

// func (m *Mysql) Metrics() []plugin.Metric {
// 	return nil
// }
