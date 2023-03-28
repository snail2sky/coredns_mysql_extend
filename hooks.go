package coredns_mysql_extend

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
)

func (m *Mysql) rePing() {
	for {
		if err := m.DB.Ping(); err != nil {
			logger.Errorf("Failed to ping database: %s", err)
			time.Sleep(m.failHeartbeatTime)
			continue
		}
		time.Sleep(m.successHeartbeatTime)
	}
}

func (m *Mysql) reGetZone() {
	zoneMap := make(map[string]int, 0)
	for {
		rows, err := m.DB.Query(m.queryZoneSQL)
		if err != nil {
			time.Sleep(m.failHeartbeatTime)
			logger.Errorf("Failed to query zones: %s", err)
			continue
		}

		for rows.Next() {
			var zoneRecord zoneRecord
			err := rows.Scan(&zoneRecord.id, &zoneRecord.name)
			if err != nil {
				logger.Error(err)
			}
			zoneMap[zoneRecord.name] = zoneRecord.id
		}
		m.zoneMap = zoneMap
		logger.Debugf("Zone %#v", zoneMap)
		time.Sleep(m.successHeartbeatTime)
	}
}

func (m *Mysql) reLoadLocalData() {
	tmpCache := make(map[record]dnsRecordInfo, 0)
	for {
		pureRecords := make([]pureRecord, 0)
		content, err := os.ReadFile(m.dumpFile)
		if err != nil {
			time.Sleep(m.failHeartbeatTime)
			return
		}
		err = json.Unmarshal(content, &pureRecords)
		if err != nil {
			time.Sleep(m.failHeartbeatTime)
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
				dnsRecordInfo := dnsRecordInfo{rrStrings: rrStrings, response: response}
				tmpCache[record] = dnsRecordInfo
			}
		}
		// TODO add lock
		m.degradeCache = tmpCache
		logger.Debug("Load degrade data from local file")
		time.Sleep(m.successHeartbeatTime)
	}
}

func (m *Mysql) onStartup() error {
	logger.Debug("On start up")
	// Initialize database connection pool
	db, err := sql.Open("mysql", m.dsn)
	if err != nil {
		openMysqlCount.With(prometheus.Labels{"status": "fail"}).Inc()
		logger.Errorf("Failed to open database: %s", err)
	} else {
		openMysqlCount.With(prometheus.Labels{"status": "success"}).Inc()
		logger.Debug("Success to open database")
	}

	// Config db connection pool
	db.SetConnMaxIdleTime(m.connMaxIdleTime)
	db.SetConnMaxLifetime(m.connMaxLifetime)
	db.SetMaxIdleConns(m.maxIdleConns)
	db.SetMaxOpenConns(m.maxOpenConns)

	m.DB = db

	// Start rePing loop
	go m.rePing()
	// start reGetZone loop
	go m.reGetZone()
	// start reLoad local file data loop
	go m.reLoadLocalData()

	m.createTables()
	return nil
}

func (m *Mysql) onShutdown() error {
	logger.Debug("on shutdown")
	if m.DB != nil {
		m.DB.Close()
	}
	// Dump memory data to local file
	m.dump2LocalData()
	return nil
}
