package coredns_mysql_extend

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/miekg/dns"
)

func (m *Mysql) dump() {
	pureRecord := make([]map[string][]string, 0)
	for record, dnsRecordInfo := range m.degradeCache {
		logger.Debugf("Record %#v", record)
		pureRecord = append(pureRecord, map[string][]string{
			fmt.Sprintf("%s%s%s", record.fqdn, keySeparator, record.Type): dnsRecordInfo.rrStrings,
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
	m.degradeCache = make(map[Record]DnsRecordInfo, 0)
	pureRecords := make([]map[string][]string, 0)
	content, err := os.ReadFile(m.DumpFile)
	if err != nil {
		return
	}
	err = json.Unmarshal(content, &pureRecords)
	if err != nil {
		return
	}
	for _, rMap := range pureRecords {
		for queryKey, rrStrings := range rMap {
			var response []dns.RR
			queryKeySlice := strings.Split(queryKey, keySeparator)
			fqdn, qType := queryKeySlice[0], queryKeySlice[1]
			record := Record{fqdn: fqdn, Type: qType}
			for _, rrString := range rrStrings {
				rr, err := dns.NewRR(rrString)
				if err != nil {
					continue
				}
				response = append(response, rr)
			}
			m.degradeCache[record] = DnsRecordInfo{rrStrings: rrStrings, response: response}
		}
	}
}
