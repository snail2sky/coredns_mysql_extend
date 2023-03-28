package coredns_mysql_extend

import (
	"github.com/coredns/coredns/plugin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Variables declared for monitoring.
var (
	openMysqlCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "open_mysql_total",
		Help:      "Counter of open mysql instance.",
	}, []string{"status"})

	createTableCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "create_table_total",
		Help:      "Counter of create table",
	}, []string{"status", "table_name"})

	degradeCacheCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "degrade_cache_total",
		Help:      "Counter of degrade cache.",
	}, []string{"option", "status", "fqdn", "qtype"})

	zoneFindCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "zone_find_total",
		Help:      "Counter of zone find.",
	}, []string{"status"})

	callNextPluginCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "call_next_plugin_total",
		Help:      "Counter of next plugin call.",
	}, []string{"fqdn", "qtype"})

	queryDBCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "query_db_total",
		Help:      "Counter of query db.",
	}, []string{"status"})

	makeAnswerCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: pluginName,
		Name:      "make_answer_total",
		Help:      "Counter of make answer count.",
	}, []string{"status"})
)
