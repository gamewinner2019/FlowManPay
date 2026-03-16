package plugin

import (
	"log"
	"sync"
)

// registry 全局插件注册表
var (
	plugins   []PluginResponder
	pluginMap map[string]PluginResponder
	mu        sync.RWMutex
	once      sync.Once
)

func init() {
	pluginMap = make(map[string]PluginResponder)
}

// Register 注册插件
func Register(p PluginResponder) {
	mu.Lock()
	defer mu.Unlock()
	key := p.Properties().Key
	if _, exists := pluginMap[key]; exists {
		log.Printf("警告: 插件 %s 已存在，将被覆盖", key)
	}
	plugins = append(plugins, p)
	pluginMap[key] = p
	log.Printf("注册插件: %s", key)
}

// GetByKey 根据key获取插件
func GetByKey(key string) PluginResponder {
	mu.RLock()
	defer mu.RUnlock()
	return pluginMap[key]
}

// GetAll 获取所有已注册插件
func GetAll() []PluginResponder {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]PluginResponder, len(plugins))
	copy(result, plugins)
	return result
}

// GetKeys 获取所有已注册插件的key列表
func GetKeys() []string {
	mu.RLock()
	defer mu.RUnlock()
	keys := make([]string, 0, len(plugins))
	for _, p := range plugins {
		keys = append(keys, p.Properties().Key)
	}
	return keys
}

// Count 返回已注册插件数量
func Count() int {
	mu.RLock()
	defer mu.RUnlock()
	return len(plugins)
}

// InitAll 初始化并注册所有插件（只执行一次）
func InitAll() {
	once.Do(func() {
		registerAllPlugins()
		log.Printf("共注册 %d 个支付插件", Count())
	})
}
