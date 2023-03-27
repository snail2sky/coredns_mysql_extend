package coredns_mysql_extend

import (
	"database/sql"
	"time"
)

func (m *Mysql) rePing() {
	for {
		if err := m.DB.Ping(); err != nil {
			logger.Errorf("Failed to ping database: %s", err)
			time.Sleep(m.RetryInterval)
			continue
		}
		time.Sleep(time.Minute)
	}
}

func (m *Mysql) reGetDomain() {
	var domainMap = make(map[string]int, 0)
	for {
		rows, err := m.DB.Query("SELECT id, name FROM " + m.DomainsTable)
		if err != nil {
			logger.Errorf("Failed to query domains: %s", err)
			time.Sleep(m.RetryInterval)
			continue
		}

		for rows.Next() {
			var domain Domain
			err := rows.Scan(&domain.ID, &domain.Name)
			if err != nil {
				logger.Error(err)
			}
			domainMap[domain.Name] = domain.ID
		}
		m.domainMap = domainMap
		logger.Debugf("domainmap %#v", domainMap)
		time.Sleep(time.Minute)
	}
}

func (m *Mysql) OnStartup() error {
	m.Once.Do(func() {
		// Initialize database connection pool
		db, err := sql.Open("mysql", m.dsn)
		if err != nil {
			logger.Errorf("Failed to open database: %s", err)
		}

		m.DB = db
		logger.Debugf("mysql %#v", m)
		// Set default values
		if m.TTL == 0 {
			m.TTL = defaultTTL
		}

		if m.RetryInterval == 0 {
			m.RetryInterval = time.Second * 5
		}
		m.load()
		// Start retry loop
		go m.rePing()
		go m.reGetDomain()
	})

	err := m.createTables()
	if err != nil {
		logger.Error(err)
	}
	logger.Debugf("Load degrade data")
	// TODO
	return nil
}

func (m *Mysql) OnShutdown() error {
	for k, v := range m.degradeCache {
		logger.Debugf("record %#v, answers %#v", k, v)
	}
	if m.DB != nil {
		m.DB.Close()
	}
	m.dump()
	return nil
}
