package coredns_mysql_extend

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/miekg/dns"
)

func (m *Mysql) dump2LocalData() {
	pureRecord := make([]pureRecord, 0)
	for record, dnsRecordInfo := range m.degradeCache {
		logger.Debugf("Record %#v", record)
		pureRecord = append(pureRecord, map[string][]string{
			fmt.Sprintf("%s%s%s", record.fqdn, keySeparator, record.qType): dnsRecordInfo.rrStrings,
		})
	}

	content, err := json.Marshal(pureRecord)
	if err != nil {
		return
	}
	if err := os.WriteFile(m.dumpFile, content, safeMode); err != nil {
		logger.Error(err)
	}
}

func (m *Mysql) loadLocalData() {
	m.degradeCache = make(map[record]dnsRecordInfo, 0)
	pureRecords := make([]pureRecord, 0)
	content, err := os.ReadFile(m.dumpFile)
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
			record := record{fqdn: fqdn, qType: qType}
			for _, rrString := range rrStrings {
				rr, err := dns.NewRR(rrString)
				if err != nil {
					continue
				}
				response = append(response, rr)
			}
			m.degradeCache[record] = dnsRecordInfo{rrStrings: rrStrings, response: response}
		}
	}
}
