package coredns_mysql_extend

import (
	"database/sql"
	"time"
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
	var zoneMap = make(map[string]int, 0)
	for {
		rows, err := m.DB.Query(zoneQuerySQL)
		if err != nil {
			logger.Errorf("Failed to query zones: %s", err)
			time.Sleep(m.failHeartbeatTime)
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

func (m *Mysql) onStartup() error {
	// Initialize database connection pool
	db, err := sql.Open("mysql", m.dsn)
	if err != nil {
		logger.Errorf("Failed to open database: %s", err)
	}

	// Config db connection pool
	db.SetConnMaxIdleTime(m.connMaxIdleTime)
	db.SetConnMaxLifetime(m.connMaxLifetime)
	db.SetMaxIdleConns(m.maxIdleConns)
	db.SetMaxOpenConns(m.maxOpenConns)

	// Load local file data
	m.loadLocalData()

	m.DB = db

	// Start retry loop
	go m.rePing()
	go m.reGetZone()

	err = m.createTables()
	if err != nil {
		logger.Error(err)
	}
	logger.Debugf("Load degrade data")
	// TODO
	return nil
}

func (m *Mysql) onShutdown() error {
	if m.DB != nil {
		m.DB.Close()
	}
	// Dump memory data to local file
	m.dump2LocalData()
	return nil
}
