package coredns_mysql_extend

import (
	"encoding/json"
	"fmt"
	"os"
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
