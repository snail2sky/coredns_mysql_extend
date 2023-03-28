package coredns_mysql_extend

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/prometheus/client_golang/prometheus"
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
		logger.Errorf("Failed to dump data to local: %s", err)
		dumpLocalData.With(prometheus.Labels{"status": "fail"}).Inc()
		return
	}
	if err := os.WriteFile(m.dumpFile, content, safeMode); err != nil {
		logger.Error(err)
		logger.Errorf("Failed to dump data to local: %s", err)
		dumpLocalData.With(prometheus.Labels{"status": "fail"}).Inc()
		return
	}
	logger.Debug("Failed to dump data to local")
	dumpLocalData.With(prometheus.Labels{"status": "success"}).Inc()
}
