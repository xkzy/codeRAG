package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ConfigGet reads a top-level dotted key from a Config and returns its string
// form. It supports bool/int/string fields and slice fields (comma-joined).
func ConfigGet(cfg Config, key string) (string, error) {
	switch strings.ToLower(key) {
	case "storage":
		return cfg.Storage, nil
	case "cache.enabled":
		return strconv.FormatBool(cfg.Cache.Enabled), nil
	case "cache.exact.max_entries":
		return strconv.Itoa(cfg.Cache.Exact.MaxEntries), nil
	case "cache.semantic.threshold":
		return fmt.Sprintf("%v", cfg.Cache.Semantic.Threshold), nil
	case "cache.semantic.max_entries":
		return strconv.Itoa(cfg.Cache.Semantic.MaxEntries), nil
	case "cache.ttl.enabled":
		return strconv.FormatBool(cfg.Cache.TTL.Enabled), nil
	case "cache.ttl.seconds":
		return strconv.Itoa(cfg.Cache.TTL.Seconds), nil
	case "indexing.incremental":
		return strconv.FormatBool(cfg.Indexing.Incremental), nil
	case "indexing.skip_graphify":
		return strconv.FormatBool(cfg.Indexing.SkipGraphify), nil
	case "watch.enabled":
		return strconv.FormatBool(cfg.Watch.Enabled), nil
	case "watch.interval":
		return cfg.Watch.Interval, nil
	case "watch.index_interval":
		return cfg.Watch.IndexInterval, nil
	case "watch.debounce":
		return cfg.Watch.Debounce, nil
	case "watch.index_on_change":
		return strconv.FormatBool(cfg.Watch.IndexOnChange), nil
	case "watch.max_workers":
		return strconv.Itoa(cfg.Watch.MaxWorkers), nil
	case "watch.queue_size":
		return strconv.Itoa(cfg.Watch.QueueSize), nil
	case "watch.max_watch_dirs":
		return strconv.Itoa(cfg.Watch.MaxWatchDirs), nil
	case "watch.max_dirty_files":
		return strconv.Itoa(cfg.Watch.MaxDirtyFiles), nil
	case "watch.project_scan_depth":
		return strconv.Itoa(cfg.Watch.ProjectScanDepth), nil
	case "watch.project_roots":
		return strings.Join(cfg.Watch.ProjectRoots, ","), nil
	case "graph.cache_nodes":
		return strconv.Itoa(cfg.Graph.CacheNodes), nil
	case "graph.cache_edges":
		return strconv.Itoa(cfg.Graph.CacheEdges), nil
	case "verification.enabled":
		return strconv.FormatBool(cfg.Verification.Enabled), nil
	case "verification.timeout_seconds":
		return strconv.Itoa(cfg.Verification.TimeoutSeconds), nil
	case "verification.max_output_bytes":
		return strconv.Itoa(cfg.Verification.MaxOutputBytes), nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

// ConfigSet writes a top-level dotted key. It parses the value into the
// appropriate type and mutates cfg in place.
func ConfigSet(cfg *Config, key, value string) error {
	switch strings.ToLower(key) {
	case "storage":
		cfg.Storage = value
	case "cache.enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Cache.Enabled = v
	case "cache.exact.max_entries":
		return setInt(&cfg.Cache.Exact.MaxEntries, value)
	case "cache.semantic.threshold":
		return setFloat(&cfg.Cache.Semantic.Threshold, value)
	case "cache.semantic.max_entries":
		return setInt(&cfg.Cache.Semantic.MaxEntries, value)
	case "cache.ttl.enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Cache.TTL.Enabled = v
	case "cache.ttl.seconds":
		return setInt(&cfg.Cache.TTL.Seconds, value)
	case "indexing.incremental":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Indexing.Incremental = v
	case "indexing.skip_graphify":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Indexing.SkipGraphify = v
	case "watch.enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Watch.Enabled = v
	case "watch.interval":
		cfg.Watch.Interval = value
	case "watch.index_interval":
		cfg.Watch.IndexInterval = value
	case "watch.debounce":
		cfg.Watch.Debounce = value
	case "watch.index_on_change":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Watch.IndexOnChange = v
	case "watch.max_workers":
		return setInt(&cfg.Watch.MaxWorkers, value)
	case "watch.queue_size":
		return setInt(&cfg.Watch.QueueSize, value)
	case "watch.max_watch_dirs":
		return setInt(&cfg.Watch.MaxWatchDirs, value)
	case "watch.max_dirty_files":
		return setInt(&cfg.Watch.MaxDirtyFiles, value)
	case "watch.project_scan_depth":
		return setInt(&cfg.Watch.ProjectScanDepth, value)
	case "watch.project_roots":
		cfg.Watch.ProjectRoots = splitCSV(value)
	case "graph.cache_nodes":
		return setInt(&cfg.Graph.CacheNodes, value)
	case "graph.cache_edges":
		return setInt(&cfg.Graph.CacheEdges, value)
	case "verification.enabled":
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		cfg.Verification.Enabled = v
	case "verification.timeout_seconds":
		return setInt(&cfg.Verification.TimeoutSeconds, value)
	case "verification.max_output_bytes":
		return setInt(&cfg.Verification.MaxOutputBytes, value)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

func setInt(p *int, v string) error {
	n, err := strconv.Atoi(v)
	if err != nil {
		return err
	}
	*p = n
	return nil
}

func setFloat(p *float64, v string) error {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return err
	}
	*p = f
	return nil
}

func splitCSV(v string) []string {
	if v == "" {
		return []string{}
	}
	out := make([]string, 0)
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}