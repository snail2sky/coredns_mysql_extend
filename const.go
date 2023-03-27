package coredns_mysql_extend

import "time"

const (
	defaultDSN                  = "root:qwer1234@tcp(127.0.0.1:3306)/dns"
	defaultDumpFile             = "dump.json"
	defaultTTL                  = 120
	defaultZonesTable           = "zones"
	defaultRecordsTable         = "records"
	defaultMaxIdleConns         = 4
	defaultMaxOpenConns         = 8
	defaultConnMaxIdleTime      = time.Hour * 1
	defaultConnMaxLifeTime      = time.Hour * 24
	defaultFailHeartBeatTime    = time.Second * 5
	defaultSuccessHeartBeatTime = time.Second * 60

	zero          = 0
	zeroTime      = zero
	safeMode      = 0640
	rootZone      = "."
	keySeparator  = ":"
	zoneSeparator = "."
	wildcard      = "*"
	cnameQtype    = "CNAME"
	pluginName    = "mysql"
)
