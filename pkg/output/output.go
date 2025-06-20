// SPDX-FileCopyrightText: © 2025 Nfrastack <code@nfrastack.com>
//
// SPDX-License-Identifier: BSD-3-Clause

package output

import (
	"herald/pkg/log"
	"herald/pkg/util"

	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// DomainConfig represents a minimal domain config for output filtering
type DomainConfig interface {
	GetOutputs() []string
}

// GlobalConfigForOutput represents the minimal config interface needed by output
type GlobalConfigForOutput interface {
	GetDomains() map[string]DomainConfig
}

// globalConfigGetter is a function that returns the global config
var globalConfigGetter func() GlobalConfigForOutput

// SetGlobalConfigGetter sets the function to retrieve global config
func SetGlobalConfigGetter(getter func() GlobalConfigForOutput) {
	globalConfigGetter = getter
}

// getGlobalConfigForOutput safely gets the global config without import cycles
func getGlobalConfigForOutput() GlobalConfigForOutput {
	if globalConfigGetter != nil {
		return globalConfigGetter()
	}
	return nil
}

// min returns the smaller of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// init automatically registers all output types when the package is imported
func init() {
	log.Debug("[output] Auto-registering core output types")

	// Register core output formats directly to avoid import cycles
	registerAllCoreFormats()
}

// registerAllCoreFormats registers all built-in output formats
func registerAllCoreFormats() {
	// Register file formats with actual implementations
	RegisterFormat("file", createFileOutputDirect)
	RegisterFormat("file/json", createJSONOutputDirect)
	RegisterFormat("file/yaml", createYAMLOutputDirect)
	RegisterFormat("file/hosts", createHostsOutputDirect)
	RegisterFormat("file/zone", createZoneOutputDirect)

	// Register remote format
	RegisterFormat("remote", createRemoteOutputDirect)

	// Register DNS format
	RegisterFormat("dns", createDNSOutput)
	RegisterFormat("dns/cloudflare", createCloudflareOutputDirect)

	log.Debug("[output] Registered core formats: file, remote, dns")
}

// createFileOutputDirect creates file outputs with dynamic loading
func createFileOutputDirect(profileName string, config map[string]interface{}) (OutputFormat, error) {
	format, ok := config["format"].(string)
	if !ok || format == "" {
		return nil, fmt.Errorf("file output requires 'format' field")
	}

	// Check if we have built-in support for this format
	switch format {
	case "json", "yaml", "hosts", "zone":
		// These are built-in formats implemented directly in this package
		return createBuiltinFileFormat(profileName, format, config)
	default:
		// Try to load external format dynamically
		return nil, fmt.Errorf("file format '%s' not supported. Built-in formats: json, yaml, hosts, zone. For external formats, import the appropriate package", format)
	}
}

// createBuiltinFileFormat creates built-in file formats without external dependencies
func createBuiltinFileFormat(profileName, format string, config map[string]interface{}) (OutputFormat, error) {
	switch format {
	case "json":
		return createJSONOutputDirect(profileName, config)
	case "yaml":
		return createYAMLOutputDirect(profileName, config)
	case "zone":
		return createZoneOutputDirect(profileName, config)
	case "hosts":
		return createHostsOutputDirect(profileName, config)
	default:
		return nil, fmt.Errorf("unsupported built-in file format: %s", format)
	}
}

// createRemoteOutputDirect creates remote outputs directly
func createRemoteOutputDirect(profileName string, config map[string]interface{}) (OutputFormat, error) {
	return createRemoteFormat(profileName, config)
}

// createJSONOutputDirect creates JSON file outputs directly
func createJSONOutputDirect(profileName string, config map[string]interface{}) (OutputFormat, error) {
	pathRaw, ok := config["path"]
	if !ok || pathRaw == nil {
		return nil, fmt.Errorf("JSON format requires 'path' field")
	}

	path := pathRaw.(string)
	path = util.ReadSecretValue(path)

	if path == "" {
		return nil, fmt.Errorf("JSON format path cannot be empty after processing")
	}

	return &jsonFormat{
		profileName: profileName,
		config:      config,
		path:        path,
		records:     make(map[string]*jsonRecord),
	}, nil
}

// createYAMLOutputDirect creates YAML file outputs directly
func createYAMLOutputDirect(profileName string, config map[string]interface{}) (OutputFormat, error) {
	pathRaw, ok := config["path"]
	if !ok || pathRaw == nil {
		return nil, fmt.Errorf("YAML format requires 'path' field")
	}

	path := pathRaw.(string)
	path = util.ReadSecretValue(path)

	if path == "" {
		return nil, fmt.Errorf("YAML format path cannot be empty after processing")
	}

	return &yamlFormat{
		profileName: profileName,
		config:      config,
		path:        path,
		records:     make(map[string]*yamlRecord),
	}, nil
}

// createHostsOutputDirect creates hosts file outputs directly
func createHostsOutputDirect(profileName string, config map[string]interface{}) (OutputFormat, error) {
	pathRaw, ok := config["path"]
	if !ok || pathRaw == nil {
		return nil, fmt.Errorf("hosts format requires 'path' field")
	}

	path := pathRaw.(string)
	path = util.ReadSecretValue(path)

	if path == "" {
		return nil, fmt.Errorf("hosts format path cannot be empty after processing")
	}

	return &hostsFormat{
		profileName: profileName,
		config:      config,
		path:        path,
		records:     make(map[string]*hostsRecord),
	}, nil
}

// createZoneOutputDirect creates zone file outputs directly
func createZoneOutputDirect(profileName string, config map[string]interface{}) (OutputFormat, error) {
	pathRaw, ok := config["path"]
	if !ok || pathRaw == nil {
		return nil, fmt.Errorf("zone format requires 'path' field")
	}

	path := pathRaw.(string)
	path = util.ReadSecretValue(path)

	if path == "" {
		return nil, fmt.Errorf("zone format path cannot be empty after processing")
	}

	return &zoneFormat{
		profileName: profileName,
		config:      config,
		path:        path,
		records:     make(map[string]*zoneRecord),
	}, nil
}

// createDNSOutput creates a DNS output instance without import cycle
func createDNSOutput(profileName string, config map[string]interface{}) (OutputFormat, error) {
	provider, ok := config["provider"].(string)
	if !ok || provider == "" {
		return nil, fmt.Errorf("dns output requires 'provider' field")
	}

	// Check if we have specific provider support
	switch provider {
	case "cloudflare":
		return createCloudflareOutputDirect(profileName, config)
	default:
		// Return placeholder for unsupported providers
		return &placeholderFormat{
			profileName: profileName,
			formatType:  fmt.Sprintf("dns/%s", provider),
		}, nil
	}
}

// createCloudflareOutputDirect creates Cloudflare DNS outputs directly
func createCloudflareOutputDirect(profileName string, config map[string]interface{}) (OutputFormat, error) {
	apiTokenRaw, ok := config["api_token"]
	if !ok || apiTokenRaw == nil {
		return nil, fmt.Errorf("cloudflare DNS requires 'api_token' field")
	}

	apiToken := apiTokenRaw.(string)
	apiToken = util.ReadSecretValue(apiToken)

	if apiToken == "" {
		return nil, fmt.Errorf("cloudflare DNS api_token cannot be empty after processing")
	}

	return &cloudflareFormat{
		profileName:    profileName,
		config:         config,
		apiToken:       apiToken,
		records:        make(map[string]*dnsRecord),
		changedRecords: make(map[string]bool),
		zoneCache:      make(map[string]string),
	}, nil
}

// Global registry for output format creators
var (
	outputFormatRegistry = make(map[string]func(string, map[string]interface{}) (OutputFormat, error))
	registryMutex        sync.RWMutex
)

// RegisterFormat registers an output format creator function
func RegisterFormat(formatName string, createFunc func(string, map[string]interface{}) (OutputFormat, error)) {
	registryMutex.Lock()
	defer registryMutex.Unlock()
	outputFormatRegistry[formatName] = createFunc
	log.Debug("[output] Registered format creator for '%s'", formatName)
}

// createFileFormat creates a file output format using dynamic loading to avoid import cycles
func createFileFormat(profileName string, config map[string]interface{}) (OutputFormat, error) {
	// Try to dynamically load the file output package
	format, _ := config["format"].(string)
	if format == "" {
		format = "unknown"
	}

	log.Debug("[output] Attempting to create file format '%s' for profile '%s'", format, profileName)

	// Check if we have a registered creator for this file format
	registryMutex.RLock()
	createFunc, exists := outputFormatRegistry["file/"+format]
	if !exists {
		createFunc, exists = outputFormatRegistry["file"]
	}
	registryMutex.RUnlock()

	if exists {
		return createFunc(profileName, config)
	}

	// Fallback to placeholder implementation
	log.Debug("[output] No registered creator for file format '%s', using placeholder", format)
	return &placeholderFormat{
		profileName: profileName,
		formatType:  fmt.Sprintf("file/%s", format),
	}, nil
}

// createDNSFormat creates a DNS output format using dynamic loading to avoid import cycles
func createDNSFormat(profileName string, config map[string]interface{}) (OutputFormat, error) {
	provider, _ := config["provider"].(string)
	if provider == "" {
		return nil, fmt.Errorf("DNS format requires 'provider' field")
	}

	log.Debug("[output] Attempting to create DNS format with provider '%s' for profile '%s'", provider, profileName)

	// Check if we have a registered creator for this DNS provider
	registryMutex.RLock()
	createFunc, exists := outputFormatRegistry["dns/"+provider]
	if !exists {
		createFunc, exists = outputFormatRegistry["dns"]
	}
	registryMutex.RUnlock()

	if exists {
		return createFunc(profileName, config)
	}

	// Fallback to placeholder implementation
	log.Debug("[output] No registered creator for DNS provider '%s', using placeholder", provider)
	return &placeholderFormat{
		profileName: profileName,
		formatType:  fmt.Sprintf("dns/%s", provider),
	}, nil
}

// GetOutputManager returns the global output manager instance
func GetOutputManager() *OutputManager {
	return GetGlobalOutputManager()
}

// OutputFormat defines the interface that all output formats must implement
type OutputFormat interface {
	GetName() string
	WriteRecord(domain, hostname, target, recordType string, ttl int) error
	WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error
	RemoveRecord(domain, hostname, recordType string) error
	Sync() error
}

// OutputManager manages multiple output formats
type OutputManager struct {
	profiles        map[string]OutputFormat
	mutex           sync.RWMutex
	syncMutex       sync.Mutex                 // Prevent concurrent sync operations
	lastSync        time.Time                  // Track last sync time
	syncCooldown    time.Duration              // Minimum time between syncs
	changedProfiles map[string]map[string]bool // Track which profiles have changes per source
	changesMutex    sync.RWMutex               // Protect changes tracking
}

// NewOutputManager creates a new output manager
func NewOutputManager() *OutputManager {
	return &OutputManager{
		profiles:        make(map[string]OutputFormat),
		syncCooldown:    2 * time.Second, // Minimum 2 seconds between syncs
		changedProfiles: make(map[string]map[string]bool),
	}
}

// Global output manager instance
var globalOutputManager *OutputManager
var globalOutputManagerMutex sync.RWMutex

// SetGlobalOutputManager sets the global output manager instance
func SetGlobalOutputManager(manager *OutputManager) {
	globalOutputManagerMutex.Lock()
	defer globalOutputManagerMutex.Unlock()
	globalOutputManager = manager
}

// GetGlobalOutputManager returns the global output manager instance
func GetGlobalOutputManager() *OutputManager {
	globalOutputManagerMutex.RLock()
	defer globalOutputManagerMutex.RUnlock()
	return globalOutputManager
}

// WriteRecordWithSourceAndDomainFilter writes a DNS record with source and domain filtering
func (om *OutputManager) WriteRecordWithSourceAndDomainFilter(domain, hostname, target, recordType string, ttl int, source string, domainManager interface{}) error {
	// For now, just call the regular method - domain filtering can be added later if needed
	return om.WriteRecordWithSource(domain, hostname, target, recordType, ttl, source)
}

// WriteRecordWithDomainFilter writes a DNS record only to outputs configured for the specific domain
func (om *OutputManager) WriteRecordWithDomainFilter(domain, hostname, target, recordType string, ttl int, source, domainConfigKey string, domainManager interface{}) error {
	// For now, we'll create a list of allowed outputs based on the domain config key
	// This avoids the import cycle by getting the info from the domain layer
	allowedOutputs := om.getAllowedOutputsForDomainConfig(domainConfigKey)

	if len(allowedOutputs) == 0 {
		log.Debug("[output/manager] No outputs configured for domain config '%s' - falling back to all outputs", domainConfigKey)
		return om.WriteRecordWithSource(domain, hostname, target, recordType, ttl, source)
	}

	om.mutex.RLock()
	defer om.mutex.RUnlock()

	writtenCount := 0
	var errors []string

	for _, outputProfile := range allowedOutputs {
		if profile, exists := om.profiles[outputProfile]; exists {
			if err := profile.WriteRecordWithSource(domain, hostname, target, recordType, ttl, source); err != nil {
				errors = append(errors, fmt.Sprintf("profile '%s': %v", outputProfile, err))
			} else {
				log.Debug("[output/manager] Successfully wrote record to profile '%s'", outputProfile)
				writtenCount++

				// Mark this profile as changed for this source
				om.changesMutex.Lock()
				if om.changedProfiles[source] == nil {
					om.changedProfiles[source] = make(map[string]bool)
				}
				om.changedProfiles[source][outputProfile] = true
				om.changesMutex.Unlock()
			}
		} else {
			log.Warn("[output/manager] Output profile '%s' not found (referenced by domain config '%s')", outputProfile, domainConfigKey)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to write to some outputs: %s", strings.Join(errors, "; "))
	}

	if writtenCount > 0 {
		log.Debug("[output/manager] Successfully wrote to %d output profiles for domain config '%s'", writtenCount, domainConfigKey)
	}

	return nil
}

// getAllowedOutputsForDomainConfig returns the output profiles allowed for a domain config
// This uses the global config to determine the allowed outputs dynamically
func (om *OutputManager) getAllowedOutputsForDomainConfig(domainConfigKey string) []string {
	// Import the config package to access global configuration
	// This is safe as config doesn't import output
	globalConfig := getGlobalConfigForOutput()
	if globalConfig == nil {
		log.Debug("[output/manager] No global config available, falling back to all outputs")
		return []string{}
	}

	// Find the domain config by key
	if domainConfig, exists := globalConfig.GetDomains()[domainConfigKey]; exists {
		// Use the GetOutputs helper method to get effective outputs
		outputs := domainConfig.GetOutputs()
		log.Debug("[output/manager] Domain config '%s' allows outputs: %v", domainConfigKey, outputs)
		return outputs
	}

	log.Debug("[output/manager] Domain config '%s' not found, falling back to all outputs", domainConfigKey)
	return []string{} // No outputs = fall back to all
}

// GetProfile returns a specific output profile by name
func (om *OutputManager) GetProfile(profileName string) OutputFormat {
	om.mutex.RLock()
	defer om.mutex.RUnlock()

	return om.profiles[profileName]
}

// AddProfile adds an output profile to the manager
func (om *OutputManager) AddProfile(profileName, format, path string, domains []string, config map[string]interface{}) error {
	om.mutex.Lock()
	defer om.mutex.Unlock()

	var outputFormat OutputFormat
	var err error

	switch format {
	case "remote":
		outputFormat, err = createRemoteFormat(profileName, config)
	case "file":
		// Use the actual file format implementation
		outputFormat, err = createFileFormat(profileName, config)
	case "dns":
		// Use the actual DNS format implementation
		outputFormat, err = createDNSFormat(profileName, config)
	case "json", "yaml", "hosts", "zone":
		// These are file formats - create them as file type
		fileConfig := make(map[string]interface{})
		for k, v := range config {
			fileConfig[k] = v
		}
		fileConfig["format"] = format
		outputFormat, err = createFileFormat(profileName, fileConfig)
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}

	if err != nil {
		return fmt.Errorf("failed to create %s format: %v", format, err)
	}

	om.profiles[profileName] = outputFormat
	return nil
}

// WriteRecord writes a DNS record to all output formats
func (om *OutputManager) WriteRecord(domain, hostname, target, recordType string, ttl int) error {
	return om.WriteRecordWithSource(domain, hostname, target, recordType, ttl, "herald")
}

// WriteRecordWithSource writes a DNS record with source information to all output formats
func (om *OutputManager) WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error {
	om.mutex.RLock()
	defer om.mutex.RUnlock()

	for profileName, outputFormat := range om.profiles {
		err := outputFormat.WriteRecordWithSource(domain, hostname, target, recordType, ttl, source)
		if err != nil {
			log.Error("[output/manager/%s] Failed to write record to profile '%s': %v", strings.ReplaceAll(domain, ".", "_"), profileName, err)
			return err
		}

		// Mark this profile as changed for this source - use more specific key
		om.changesMutex.Lock()
		if om.changedProfiles[source] == nil {
			om.changedProfiles[source] = make(map[string]bool)
		}
		om.changedProfiles[source][profileName] = true
		om.changesMutex.Unlock()
	}
	return nil
}

// RemoveRecord removes a DNS record from all output formats
func (om *OutputManager) RemoveRecord(domain, hostname, recordType string) error {
	om.mutex.RLock()
	defer om.mutex.RUnlock()

	for profileName, outputFormat := range om.profiles {
		err := outputFormat.RemoveRecord(domain, hostname, recordType)
		if err != nil {
			log.Error("[output/manager/%s] Failed to remove record from profile '%s': %v", strings.ReplaceAll(domain, ".", "_"), profileName, err)
			return err
		}
	}
	return nil
}

// SyncAll syncs all output formats
func (om *OutputManager) SyncAll() error {
	// Prevent concurrent sync operations
	om.syncMutex.Lock()
	defer om.syncMutex.Unlock()

	// Check if we're within the cooldown period
	if time.Since(om.lastSync) < om.syncCooldown {
		log.Debug("[output/manager] Sync throttled - last sync was %v ago (cooldown: %v)",
			time.Since(om.lastSync), om.syncCooldown)
		return nil
	}

	om.mutex.RLock()
	defer om.mutex.RUnlock()

	// Get list of changed profiles from all sources
	om.changesMutex.RLock()
	changedProfilesSet := make(map[string]bool)
	for _, profileMap := range om.changedProfiles {
		for profileName, changed := range profileMap {
			if changed {
				changedProfilesSet[profileName] = true
			}
		}
	}
	om.changesMutex.RUnlock()

	if len(changedProfilesSet) == 0 {
		log.Debug("[output/manager] No changed profiles to sync")
		return nil
	}

	changedProfiles := make([]string, 0, len(changedProfilesSet))
	for profileName := range changedProfilesSet {
		changedProfiles = append(changedProfiles, profileName)
	}

	log.Debug("[output/manager] Starting sync for %d changed output profiles: %v", len(changedProfiles), changedProfiles)

	for _, profileName := range changedProfiles {
		if outputFormat, exists := om.profiles[profileName]; exists {
			log.Debug("[output/manager] Syncing changed profile: %s", profileName)
			err := outputFormat.Sync()
			if err != nil {
				log.Error("[output/manager] Failed to sync profile '%s': %v", profileName, err)
				return err
			}
			log.Debug("[output/manager] Successfully synced profile: %s", profileName)
		}
	}

	// Clear change tracking after successful sync
	om.changesMutex.Lock()
	om.changedProfiles = make(map[string]map[string]bool)
	om.changesMutex.Unlock()

	// Update last sync time
	om.lastSync = time.Now()

	log.Debug("[output/manager] Completed sync for %d changed profiles", len(changedProfiles))
	return nil
}

// SyncAllFromSource syncs only output formats that have changes from a specific source
func (om *OutputManager) SyncAllFromSource(source string) error {
	// Prevent concurrent sync operations
	om.syncMutex.Lock()
	defer om.syncMutex.Unlock()

	// Check if we're within the cooldown period
	if time.Since(om.lastSync) < om.syncCooldown {
		log.Debug("[output/manager] Sync throttled for source '%s' - last sync was %v ago (cooldown: %v)",
			source, time.Since(om.lastSync), om.syncCooldown)
		return nil
	}

	om.mutex.RLock()
	defer om.mutex.RUnlock()

	// Get list of changed profiles for this specific source
	om.changesMutex.RLock()
	sourceChanges, exists := om.changedProfiles[source]
	if !exists || len(sourceChanges) == 0 {
		om.changesMutex.RUnlock()
		log.Debug("[output/manager] No changed profiles to sync for source '%s'", source)
		return nil
	}

	changedProfiles := make([]string, 0, len(sourceChanges))
	for profileName, changed := range sourceChanges {
		if changed {
			changedProfiles = append(changedProfiles, profileName)
		}
	}
	om.changesMutex.RUnlock()

	if len(changedProfiles) == 0 {
		log.Debug("[output/manager] No changed profiles to sync for source '%s'", source)
		return nil
	}

	log.Debug("[output/manager] Starting sync for %d changed output profiles from source '%s': %v", len(changedProfiles), source, changedProfiles)

	for _, profileName := range changedProfiles {
		if outputFormat, exists := om.profiles[profileName]; exists {
			log.Debug("[output/manager] Syncing changed profile: %s (source: %s)", profileName, source)
			err := outputFormat.Sync()
			if err != nil {
				log.Error("[output/manager] Failed to sync profile '%s' from source '%s': %v", profileName, source, err)
				return err
			}
			log.Debug("[output/manager] Successfully synced profile: %s (source: %s)", profileName, source)
		}
	}

	// Clear change tracking for this source after successful sync
	om.changesMutex.Lock()
	delete(om.changedProfiles, source)
	om.changesMutex.Unlock()

	// Update last sync time
	om.lastSync = time.Now()

	log.Debug("[output/manager] Completed sync for %d changed profiles from source '%s'", len(changedProfiles), source)
	return nil
}

// InitializeOutputManagerWithProfiles initializes the output manager with specific profiles from config
func InitializeOutputManagerWithProfiles(outputConfigs map[string]interface{}, enabledProfiles []string) error {
	outputManager := NewOutputManager()

	log.Trace("[output] Starting output manager initialization with profiles: %v", enabledProfiles)

	if outputConfigs != nil {
		log.Debug("[output] Found outputs configuration")

		// Create a set for faster lookup
		enabledSet := make(map[string]bool)
		for _, profile := range enabledProfiles {
			enabledSet[profile] = true
		}

		// Register all profiles from config, not just enabledProfiles
		for profileName, profileConfig := range outputConfigs {
			log.Debug("[output] Processing profile: %s", profileName)

			if configMap, ok := profileConfig.(map[string]interface{}); ok {
				// Determine output type and format
				outputType, _ := configMap["type"].(string)
				format, _ := configMap["format"].(string)

				if outputType == "" && format != "" {
					// Legacy: infer type from format
					outputType = format
				}

				if format == "" {
					format = outputType
				}

				log.Debug("[output] Determined format for %s: '%s' (type: %s)", profileName, format, outputType)

				path, _ := configMap["path"].(string)
				var domains []string

				err := outputManager.AddProfile(profileName, format, path, domains, configMap)
				if err != nil {
					log.Error("[output] Failed to add output profile '%s': %v", profileName, err)
					return err
				} else {
					log.Verbose("[output] Registered output profile '%s' (%s)", profileName, format)
				}
			} else {
				log.Warn("[output] Profile '%s' has invalid configuration type", profileName)
			}
		}
	} else {
		log.Debug("[output] No outputs configuration found")
	}

	SetGlobalOutputManager(outputManager)
	return nil
}

// createRemoteFormat creates a remote output format
func createRemoteFormat(profileName string, config map[string]interface{}) (OutputFormat, error) {
	urlRaw, ok := config["url"]
	if !ok || urlRaw == nil {
		return nil, fmt.Errorf("remote format requires 'url' field")
	}

	url := urlRaw.(string)
	url = util.ReadSecretValue(url)

	if url == "" {
		return nil, fmt.Errorf("remote format URL cannot be empty after processing")
	}

	return &remoteFormat{
		profileName: profileName,
		config:      config,
		url:         url,
		records:     make(map[string]*remoteRecord),
	}, nil
}

// createPlaceholderFileFormat creates a placeholder file format that logs operations but doesn't write files
func createPlaceholderFileFormat(profileName string, config map[string]interface{}) (OutputFormat, error) {
	return &placeholderFormat{
		profileName: profileName,
		formatType:  "file",
	}, nil
}

// createPlaceholderDNSFormat creates a placeholder DNS format that logs operations but doesn't make DNS calls
func createPlaceholderDNSFormat(profileName string, config map[string]interface{}) (OutputFormat, error) {
	return &placeholderFormat{
		profileName: profileName,
		formatType:  "dns",
	}, nil
}

// placeholderFormat is a placeholder implementation for formats that aren't fully implemented
type placeholderFormat struct {
	profileName string
	formatType  string
}

func (p *placeholderFormat) GetName() string { return p.formatType }

func (p *placeholderFormat) WriteRecord(domain, hostname, target, recordType string, ttl int) error {
	return p.WriteRecordWithSource(domain, hostname, target, recordType, ttl, "herald")
}

func (p *placeholderFormat) WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error {
	log.Debug("[output/%s/%s] Would write record: %s %s -> %s (TTL: %d, Source: %s)",
		p.formatType, strings.ReplaceAll(domain, ".", "_"), hostname, recordType, target, ttl, source)
	return nil
}

func (p *placeholderFormat) RemoveRecord(domain, hostname, recordType string) error {
	log.Debug("[output/%s/%s] Would remove record: %s %s",
		p.formatType, strings.ReplaceAll(domain, ".", "_"), hostname, recordType)
	return nil
}

func (p *placeholderFormat) Sync() error {
	log.Debug("[output/%s] Would sync %s format (placeholder implementation)", p.formatType, p.formatType)
	return nil
}

// jsonRecord represents a single DNS record for JSON output
type jsonRecord struct {
	Domain     string `json:"domain"`
	Hostname   string `json:"hostname"`
	Target     string `json:"target"`
	RecordType string `json:"type"`
	TTL        int    `json:"ttl"`
	Source     string `json:"source,omitempty"`
}

// jsonFormat implements JSON file output
type jsonFormat struct {
	profileName string
	config      map[string]interface{}
	path        string
	records     map[string]*jsonRecord
	mutex       sync.RWMutex
}

func (j *jsonFormat) GetName() string { return "json" }

func (j *jsonFormat) WriteRecord(domain, hostname, target, recordType string, ttl int) error {
	return j.WriteRecordWithSource(domain, hostname, target, recordType, ttl, "herald")
}

func (j *jsonFormat) WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)
	j.records[key] = &jsonRecord{
		Domain:     domain,
		Hostname:   hostname,
		Target:     target,
		RecordType: recordType,
		TTL:        ttl,
		Source:     source,
	}

	log.Debug("[output/json/%s] Added record: %s %s -> %s (TTL: %d)", j.profileName, hostname, recordType, target, ttl)
	return nil
}

func (j *jsonFormat) RemoveRecord(domain, hostname, recordType string) error {
	j.mutex.Lock()
	defer j.mutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)
	delete(j.records, key)
	return nil
}

func (j *jsonFormat) Sync() error {
	j.mutex.RLock()
	defer j.mutex.RUnlock()

	if len(j.records) == 0 {
		log.Debug("[output/json] No records to write")
		return nil
	}

	// Build the structured format with metadata and domains
	output := struct {
		Metadata map[string]string `json:"metadata"`
		Domains  map[string]struct {
			Comment string `json:"comment"`
			Records []struct {
				Hostname  string `json:"hostname"`
				Type      string `json:"type"`
				Target    string `json:"target"`
				TTL       int    `json:"ttl"`
				CreatedAt string `json:"created_at"`
				Source    string `json:"source"`
			} `json:"records"`
		} `json:"domains"`
	}{
		Metadata: map[string]string{
			"generator":    "herald",
			"generated_at": time.Now().Format(time.RFC3339),
			"last_updated": time.Now().Format(time.RFC3339),
		},
		Domains: make(map[string]struct {
			Comment string `json:"comment"`
			Records []struct {
				Hostname  string `json:"hostname"`
				Type      string `json:"type"`
				Target    string `json:"target"`
				TTL       int    `json:"ttl"`
				CreatedAt string `json:"created_at"`
				Source    string `json:"source"`
			} `json:"records"`
		}),
	}

	// Group records by domain
	for _, record := range j.records {
		domain := record.Domain
		if _, exists := output.Domains[domain]; !exists {
			output.Domains[domain] = struct {
				Comment string `json:"comment"`
				Records []struct {
					Hostname  string `json:"hostname"`
					Type      string `json:"type"`
					Target    string `json:"target"`
					TTL       int    `json:"ttl"`
					CreatedAt string `json:"created_at"`
					Source    string `json:"source"`
				} `json:"records"`
			}{
				Comment: fmt.Sprintf("Domain: %s", domain),
				Records: []struct {
					Hostname  string `json:"hostname"`
					Type      string `json:"type"`
					Target    string `json:"target"`
					TTL       int    `json:"ttl"`
					CreatedAt string `json:"created_at"`
					Source    string `json:"source"`
				}{},
			}
		}

		// Add record to domain
		domainData := output.Domains[domain]
		domainData.Records = append(domainData.Records, struct {
			Hostname  string `json:"hostname"`
			Type      string `json:"type"`
			Target    string `json:"target"`
			TTL       int    `json:"ttl"`
			CreatedAt string `json:"created_at"`
			Source    string `json:"source"`
		}{
			Hostname:  record.Hostname,
			Type:      record.RecordType,
			Target:    record.Target,
			TTL:       record.TTL,
			CreatedAt: time.Now().Format(time.RFC3339),
			Source:    record.Source,
		})
		output.Domains[domain] = domainData
	}

	// Marshal to JSON
	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	// Write to file
	err = writeFileWithBackup(j.path, jsonBytes)
	if err != nil {
		return fmt.Errorf("failed to write JSON file %s: %v", j.path, err)
	}

	log.Info("[output/json] Successfully wrote %d records to %s", len(j.records), j.path)
	return nil
}

// writeFileWithBackup writes data to a file with atomic operation
func writeFileWithBackup(path string, data []byte) error {
	// For now, just write directly - can add atomic writes later
	return os.WriteFile(path, data, 0644)
}

// yamlRecord represents a single DNS record for YAML output
type yamlRecord struct {
	Domain     string `yaml:"domain"`
	Hostname   string `yaml:"hostname"`
	Target     string `yaml:"target"`
	RecordType string `yaml:"type"`
	TTL        int    `yaml:"ttl"`
	Source     string `yaml:"source,omitempty"`
}

// yamlFormat implements YAML file output
type yamlFormat struct {
	profileName string
	config      map[string]interface{}
	path        string
	records     map[string]*yamlRecord
	mutex       sync.RWMutex
}

func (y *yamlFormat) GetName() string { return "yaml" }

func (y *yamlFormat) WriteRecord(domain, hostname, target, recordType string, ttl int) error {
	return y.WriteRecordWithSource(domain, hostname, target, recordType, ttl, "herald")
}

func (y *yamlFormat) WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error {
	y.mutex.Lock()
	defer y.mutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)
	y.records[key] = &yamlRecord{
		Domain:     domain,
		Hostname:   hostname,
		Target:     target,
		RecordType: recordType,
		TTL:        ttl,
		Source:     source,
	}

	log.Debug("[output/yaml/%s] Added record: %s %s -> %s (TTL: %d)", y.profileName, hostname, recordType, target, ttl)
	return nil
}

func (y *yamlFormat) RemoveRecord(domain, hostname, recordType string) error {
	y.mutex.Lock()
	defer y.mutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)
	delete(y.records, key)
	return nil
}

func (y *yamlFormat) Sync() error {
	y.mutex.RLock()
	defer y.mutex.RUnlock()

	if len(y.records) == 0 {
		log.Debug("[output/yaml] No records to write")
		return nil
	}

	// Build YAML content with metadata and domains structure
	content := "metadata:\n"
	content += "  generator: herald\n"
	content += fmt.Sprintf("  generated_at: \"%s\"\n", time.Now().Format(time.RFC3339))
	content += fmt.Sprintf("  last_updated: \"%s\"\n", time.Now().Format(time.RFC3339))
	content += "\n"
	content += "domains:\n"

	// Group records by domain
	domainRecords := make(map[string][]*yamlRecord)
	for _, record := range y.records {
		domain := record.Domain
		domainRecords[domain] = append(domainRecords[domain], record)
	}

	// Write each domain
	for domain, records := range domainRecords {
		content += fmt.Sprintf("  %s:\n", domain)
		content += fmt.Sprintf("    comment: \"Domain: %s\"\n", domain)
		content += "    records:\n"

		for _, record := range records {
			content += fmt.Sprintf("      - hostname: %s\n", record.Hostname)
			content += fmt.Sprintf("        type: %s\n", record.RecordType)
			content += fmt.Sprintf("        target: %s\n", record.Target)
			content += fmt.Sprintf("        ttl: %d\n", record.TTL)
			content += fmt.Sprintf("        created_at: \"%s\"\n", time.Now().Format(time.RFC3339))
			content += fmt.Sprintf("        source: %s\n", record.Source)
			content += "\n"
		}
	}

	// Write to file
	err := writeFileWithBackup(y.path, []byte(content))
	if err != nil {
		return fmt.Errorf("failed to write YAML file %s: %v", y.path, err)
	}

	log.Info("[output/yaml/%s] Successfully wrote %d records to %s", y.profileName, len(y.records), y.path)
	return nil
}

// remoteRecord represents a single DNS record for remote output
type remoteRecord struct {
	Domain     string `json:"domain"`
	Hostname   string `json:"hostname"`
	Target     string `json:"target"`
	RecordType string `json:"type"`
	TTL        int    `json:"ttl"`
	Source     string `json:"source,omitempty"`
}

// remoteFormat implements remote output via HTTP POST
type remoteFormat struct {
	profileName string
	config      map[string]interface{}
	url         string
	records     map[string]*remoteRecord
	mutex       sync.RWMutex
}

func (r *remoteFormat) GetName() string { return "remote" }

func (r *remoteFormat) WriteRecord(domain, hostname, target, recordType string, ttl int) error {
	return r.WriteRecordWithSource(domain, hostname, target, recordType, ttl, "herald")
}

func (r *remoteFormat) WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)
	r.records[key] = &remoteRecord{
		Domain:     domain,
		Hostname:   hostname,
		Target:     target,
		RecordType: recordType,
		TTL:        ttl,
		Source:     source,
	}

	log.Debug("[output/remote/%s] Added record: %s %s -> %s (TTL: %d)", strings.ReplaceAll(domain, ".", "_"), hostname, recordType, target, ttl)
	return nil
}

func (r *remoteFormat) RemoveRecord(domain, hostname, recordType string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)
	delete(r.records, key)
	return nil
}

func (r *remoteFormat) Sync() error {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	if len(r.records) == 0 {
		log.Debug("[output/remote] No records to send, skipping HTTP POST")
		return nil
	}

	// Build the payload in the correct API format
	payload := struct {
		Metadata map[string]string                 `json:"metadata"`
		Domains  map[string]map[string]interface{} `json:"domains"`
	}{
		Metadata: map[string]string{
			"generator":    "herald",
			"generated_at": time.Now().Format(time.RFC3339),
			"last_updated": time.Now().Format(time.RFC3339),
		},
		Domains: make(map[string]map[string]interface{}),
	}

	// Group records by domain
	for _, record := range r.records {
		domain := record.Domain
		if _, exists := payload.Domains[domain]; !exists {
			payload.Domains[domain] = map[string]interface{}{
				"comment": fmt.Sprintf("Domain: %s", domain),
				"records": []map[string]interface{}{},
			}
		}

		// Add record to domain
		domainData := payload.Domains[domain]
		records := domainData["records"].([]map[string]interface{})
		records = append(records, map[string]interface{}{
			"hostname":   record.Hostname,
			"type":       record.RecordType,
			"target":     record.Target,
			"ttl":        record.TTL,
			"created_at": time.Now().Format(time.RFC3339),
			"source":     record.Source,
		})
		domainData["records"] = records
		payload.Domains[domain] = domainData
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	// Get auth config from remote config
	var clientID, token string
	if val, exists := r.config["client_id"]; exists {
		if strVal, ok := val.(string); ok {
			clientID = util.ReadSecretValue(strVal)
		}
	}
	if val, exists := r.config["token"]; exists {
		if strVal, ok := val.(string); ok {
			token = util.ReadSecretValue(strVal)
		}
	}

	// Send HTTP POST request with authentication
	req, err := http.NewRequest("POST", r.url, strings.NewReader(string(jsonBytes)))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication headers
	if clientID != "" {
		req.Header.Set("X-Client-ID", clientID)
		log.Debug("[output/remote] Setting X-Client-ID: %s", clientID)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		log.Debug("[output/remote] Setting Authorization header with token: %s", util.MaskSensitiveValue(token))
	}

	log.Debug("[output/remote] Sending %d records to %s with client_id: %s", len(r.records), r.url, clientID)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Error("[output/remote] Remote server returned status %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))
	}

	log.Info("[output/remote] Successfully sent %d records to %s", len(r.records), r.url)

	// Clear records after successful sync
	r.records = make(map[string]*remoteRecord)
	return nil
}

// hostsRecord represents a single DNS record for hosts file output
type hostsRecord struct {
	Domain     string
	Hostname   string
	Target     string
	RecordType string
	TTL        int
	Source     string
	ResolvedIP string // For hosts file, we need the actual IP
}

// hostsFormat implements hosts file output
type hostsFormat struct {
	profileName string
	config      map[string]interface{}
	path        string
	records     map[string]*hostsRecord
	mutex       sync.RWMutex
}

func (h *hostsFormat) GetName() string { return "hosts" }

func (h *hostsFormat) WriteRecord(domain, hostname, target, recordType string, ttl int) error {
	return h.WriteRecordWithSource(domain, hostname, target, recordType, ttl, "herald")
}

func (h *hostsFormat) WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	key := fmt.Sprintf("%s:%s", domain, hostname)
	fqdn := hostname + "." + domain
	if hostname == "@" || hostname == "" {
		fqdn = domain
	}

	// Handle CNAME flattening and external resolution based on configuration
	resolvedIP := target
	if recordType == "CNAME" {
		// Check if ip_override is configured first
		if ipOverride, exists := h.config["ip_override"]; exists {
			if ipOverrideStr, ok := ipOverride.(string); ok && ipOverrideStr != "" {
				resolvedIP = ipOverrideStr
				recordType = "A" // Change to A record since we're overriding
				log.Debug("[output/hosts/%s] Using IP override %s for CNAME %s -> %s", strings.ReplaceAll(domain, ".", "_"), ipOverrideStr, fqdn, target)
			}
		} else if flattenCNAMEs, exists := h.config["flatten_cnames"]; exists && flattenCNAMEs == true {
			// Check if resolve_external is enabled
			if resolveExternal, exists := h.config["resolve_external"]; exists && resolveExternal == true {
				if resolved := h.resolveCNAME(target); resolved != "" {
					resolvedIP = resolved
					recordType = "A" // Change to A record since we resolved it
					log.Debug("[output/hosts/%s] Flattened CNAME %s -> %s to IP %s", strings.ReplaceAll(domain, ".", "_"), fqdn, target, resolvedIP)
				} else {
					log.Warn("[output/hosts/%s] Failed to resolve CNAME target %s for %s", strings.ReplaceAll(domain, ".", "_"), target, fqdn)
				}
			} else {
				log.Debug("[output/hosts/%s] CNAME flattening enabled but resolve_external disabled for %s", strings.ReplaceAll(domain, ".", "_"), fqdn)
			}
		} else {
			log.Debug("[output/hosts/%s] CNAME flattening disabled for %s", strings.ReplaceAll(domain, ".", "_"), fqdn)
		}
	}

	h.records[key] = &hostsRecord{
		Domain:     domain,
		Hostname:   hostname,
		Target:     target,
		RecordType: recordType,
		TTL:        ttl,
		Source:     source,
		ResolvedIP: resolvedIP,
	}

	log.Debug("[output/hosts/%s] Added record: %s -> %s (TTL: %d)", strings.ReplaceAll(domain, ".", "_"), fqdn, resolvedIP, ttl)
	return nil
}

func (h *hostsFormat) RemoveRecord(domain, hostname, recordType string) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	key := fmt.Sprintf("%s:%s", domain, hostname)
	delete(h.records, key)
	return nil
}

func (h *hostsFormat) Sync() error {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	if len(h.records) == 0 {
		log.Debug("[output/hosts] No records to write")
		return nil
	}

	// Get header comment from config
	headerComment := "Managed by Herald"
	if comment, exists := h.config["header_comment"]; exists {
		if commentStr, ok := comment.(string); ok {
			headerComment = commentStr
		}
	}

	// Build hosts file content
	content := fmt.Sprintf("# %s\n", headerComment)
	content += "# Generated: " + time.Now().Format(time.RFC3339) + "\n"
	content += fmt.Sprintf("# Records: %d unique hostnames\n", len(h.records))
	content += "\n"

	for _, record := range h.records {
		fqdn := record.Hostname + "." + record.Domain
		if record.Hostname == "@" || record.Hostname == "" {
			fqdn = record.Domain
		}

		// Use resolved IP if available, otherwise use target
		ipAddress := record.ResolvedIP
		if ipAddress == "" {
			ipAddress = record.Target
		}

		// For hosts file format: IP hostname
		if record.RecordType == "A" || record.RecordType == "AAAA" {
			content += fmt.Sprintf("%s\t%s\n", ipAddress, fqdn)
		} else if record.RecordType == "CNAME" {
			// Check if we have a resolved IP
			if record.ResolvedIP != "" && record.ResolvedIP != record.Target {
				// CNAME was successfully flattened to IP
				content += fmt.Sprintf("%s\t%s\n", record.ResolvedIP, fqdn)
			} else {
				// CNAME not resolved - comment it out
				content += fmt.Sprintf("# %s\t%s  # CNAME -> %s (unresolved)\n", "0.0.0.0", fqdn, record.Target)
			}
		} else {
			// Other record types - try to use as IP or comment out
			if ip := net.ParseIP(ipAddress); ip != nil {
				content += fmt.Sprintf("%s\t%s\n", ipAddress, fqdn)
			} else {
				content += fmt.Sprintf("# %s\t%s  # %s -> %s (not an IP)\n", "0.0.0.0", fqdn, record.RecordType, record.Target)
			}
		}
	}

	// Write to file
	err := writeFileWithBackup(h.path, []byte(content))
	if err != nil {
		return fmt.Errorf("failed to write hosts file %s: %v", h.path, err)
	}

	log.Info("[output/hosts/%s] Successfully wrote %d records to %s", h.profileName, len(h.records), h.path)
	return nil
}

// resolveCNAME resolves a CNAME target to an IP address using configured DNS server
func (h *hostsFormat) resolveCNAME(target string) string {
	// Get DNS server from config, default to system resolver
	dnsServer := "system"
	if server, exists := h.config["dns_server"]; exists {
		if serverStr, ok := server.(string); ok {
			dnsServer = serverStr
		}
	}

	// Use custom DNS server if specified
	if dnsServer != "system" && dnsServer != "" {
		// Use custom resolver with specified DNS server
		return h.resolveWithCustomDNS(target, dnsServer)
	}

	// Use system resolver
	ips, err := net.LookupIP(target)
	if err != nil {
		log.Debug("[output/hosts] Failed to resolve %s using system resolver: %v", target, err)
		return ""
	}

	log.Debug("[output/hosts] Found %d IP addresses for %s:", len(ips), target)
	for i, ip := range ips {
		log.Debug("[output/hosts]   [%d] %s (IPv4: %v)", i, ip.String(), ip.To4() != nil)
	}

	// Check if IPv4 is enabled
	enableIPv4 := true
	if enable, exists := h.config["enable_ipv4"]; exists {
		if enableBool, ok := enable.(bool); ok {
			enableIPv4 = enableBool
		}
	}

	// Check if IPv6 is enabled
	enableIPv6 := false
	if enable, exists := h.config["enable_ipv6"]; exists {
		if enableBool, ok := enable.(bool); ok {
			enableIPv6 = enableBool
		}
	}

	// Return the first matching IP address based on preferences
	for _, ip := range ips {
		if ip.To4() != nil && enableIPv4 {
			log.Debug("[output/hosts] Resolved %s to IPv4 %s", target, ip.String())
			return ip.String()
		} else if ip.To4() == nil && enableIPv6 {
			log.Debug("[output/hosts] Resolved %s to IPv6 %s", target, ip.String())
			return ip.String()
		}
	}

	log.Debug("[output/hosts] No suitable IP address found for %s (IPv4=%v, IPv6=%v)", target, enableIPv4, enableIPv6)
	return ""
}

// resolveWithCustomDNS resolves using a specific DNS server
func (h *hostsFormat) resolveWithCustomDNS(target, dnsServer string) string {
	log.Debug("[output/hosts] Resolving %s using external DNS server %s", target, dnsServer)

	// Use the miekg/dns library for external DNS resolution
	c := dns.Client{
		Timeout: 10 * time.Second,
	}

	m := dns.Msg{}
	m.SetQuestion(dns.Fqdn(target), dns.TypeA)
	m.RecursionDesired = true

	// Query the external DNS server
	r, _, err := c.Exchange(&m, dnsServer+":53")
	if err != nil {
		log.Debug("[output/hosts] DNS query failed for %s using server %s: %v", target, dnsServer, err)
		return ""
	}

	if r.Rcode != dns.RcodeSuccess {
		log.Debug("[output/hosts] DNS query returned error code %d for %s using server %s", r.Rcode, target, dnsServer)
		return ""
	}

	// Extract A records from the response
	for _, ans := range r.Answer {
		if a, ok := ans.(*dns.A); ok {
			ip := a.A.String()
			log.Debug("[output/hosts] External DNS resolution: %s -> %s (via %s)", target, ip, dnsServer)
			return ip
		}
	}

	log.Debug("[output/hosts] No A records found for %s using DNS server %s", target, dnsServer)
	return ""
}

// zoneRecord represents a single DNS record for zone file output
type zoneRecord struct {
	Domain     string
	Hostname   string
	Target     string
	RecordType string
	TTL        int
	Source     string
}

// zoneFormat implements DNS zone file output
type zoneFormat struct {
	profileName string
	config      map[string]interface{}
	path        string
	records     map[string]*zoneRecord
	mutex       sync.RWMutex
}

func (z *zoneFormat) GetName() string { return "zone" }

func (z *zoneFormat) WriteRecord(domain, hostname, target, recordType string, ttl int) error {
	return z.WriteRecordWithSource(domain, hostname, target, recordType, ttl, "herald")
}

func (z *zoneFormat) WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error {
	z.mutex.Lock()
	defer z.mutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)
	z.records[key] = &zoneRecord{
		Domain:     domain,
		Hostname:   hostname,
		Target:     target,
		RecordType: recordType,
		TTL:        ttl,
		Source:     source,
	}

	log.Debug("[output/zone/%s] Added record: %s %s -> %s (TTL: %d)", strings.ReplaceAll(domain, ".", "_"), hostname, recordType, target, ttl)
	return nil
}

func (z *zoneFormat) RemoveRecord(domain, hostname, recordType string) error {
	z.mutex.Lock()
	defer z.mutex.Unlock()

	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)
	delete(z.records, key)
	return nil
}

func (z *zoneFormat) Sync() error {
	z.mutex.RLock()
	defer z.mutex.RUnlock()

	if len(z.records) == 0 {
		log.Debug("[output/zone] No records to write")
		return nil
	}

	// Get the first domain to use for path substitution
	firstDomain := ""
	for _, record := range z.records {
		if firstDomain == "" {
			firstDomain = record.Domain
			break
		}
	}

	// Substitute %domain% placeholder in the path
	actualPath := z.path
	if firstDomain != "" {
		// Replace dots with underscores for filename
		domainForFilename := strings.ReplaceAll(firstDomain, ".", "_")
		actualPath = strings.ReplaceAll(actualPath, "%domain%", domainForFilename)
	}

	// Build zone file content
	content := "; Managed by Herald\n"
	content += "; Generated: " + time.Now().Format(time.RFC3339) + "\n"
	content += fmt.Sprintf("; Records: %d entries\n", len(z.records))
	content += "\n"

	// Add SOA record (basic one)
	if firstDomain != "" {
		content += fmt.Sprintf("$ORIGIN %s.\n", firstDomain)
		content += fmt.Sprintf("@\tIN\tSOA\tns1.%s. admin.%s. (\n", firstDomain, firstDomain)
		content += fmt.Sprintf("\t\t\t%d\t; serial (timestamp)\n", time.Now().Unix())
		content += "\t\t\t3600\t; refresh\n"
		content += "\t\t\t1800\t; retry\n"
		content += "\t\t\t604800\t; expire\n"
		content += "\t\t\t86400\t; minimum TTL\n"
		content += "\t\t\t)\n\n"
	}

	// Add DNS records
	for _, record := range z.records {
		hostname := record.Hostname
		if hostname == "@" || hostname == "" {
			hostname = "@"
		}

		// Zone file format: NAME TTL CLASS TYPE RDATA
		if record.RecordType == "CNAME" && hostname != "@" {
			// CNAME records need the dot at the end
			target := record.Target
			if !strings.HasSuffix(target, ".") {
				target += "."
			}
			content += fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", hostname, record.TTL, record.RecordType, target)
		} else if record.RecordType == "A" || record.RecordType == "AAAA" {
			content += fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", hostname, record.TTL, record.RecordType, record.Target)
		} else {
			// Generic record
			content += fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n", hostname, record.TTL, record.RecordType, record.Target)
		}
	}

	// Write to file
	err := writeFileWithBackup(actualPath, []byte(content))
	if err != nil {
		return fmt.Errorf("failed to write zone file %s: %v", actualPath, err)
	}

	log.Info("[output/zone/%s] Successfully wrote %d records to %s", z.profileName, len(z.records), actualPath)
	return nil
}

// dnsRecord represents a single DNS record for DNS provider output
type dnsRecord struct {
	Domain     string
	Hostname   string
	Target     string
	RecordType string
	TTL        int
	Source     string
	ID         string // Provider-specific record ID
}

// cloudflareFormat implements Cloudflare DNS API output
type cloudflareFormat struct {
	profileName    string
	config         map[string]interface{}
	apiToken       string
	records        map[string]*dnsRecord
	changedRecords map[string]bool // Track which records changed
	mutex          sync.RWMutex
	syncMutex      sync.Mutex        // Prevent concurrent syncs
	zoneCache      map[string]string // Cache zone IDs to reduce API calls
	zoneMutex      sync.RWMutex
}

func (c *cloudflareFormat) GetName() string { return "cloudflare" }

func (c *cloudflareFormat) WriteRecord(domain, hostname, target, recordType string, ttl int) error {
	return c.WriteRecordWithSource(domain, hostname, target, recordType, ttl, "herald")
}

func (c *cloudflareFormat) WriteRecordWithSource(domain, hostname, target, recordType string, ttl int, source string) error {
	c.mutex.Lock()
	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)

	// Check if this record actually changed
	existingRecord, exists := c.records[key]
	recordChanged := !exists ||
		existingRecord.Target != target ||
		existingRecord.TTL != ttl ||
		existingRecord.Source != source

	if !recordChanged {
		c.mutex.Unlock()
		log.Debug("[output/dns/cloudflare] Record %s.%s unchanged, skipping", hostname, domain)
		return nil
	}

	newRecord := &dnsRecord{
		Domain:     domain,
		Hostname:   hostname,
		Target:     target,
		RecordType: recordType,
		TTL:        ttl,
		Source:     source,
		ID:         "", // Will be set after API call
	}

	c.records[key] = newRecord
	c.mutex.Unlock()

	// Immediately sync only this record to prevent batching with other changes
	log.Debug("[output/dns/cloudflare] Immediately syncing record: %s.%s %s -> %s (TTL: %d, Source: %s)",
		hostname, domain, recordType, target, ttl, source)

	return c.syncSingleRecord(newRecord)
}

func (c *cloudflareFormat) RemoveRecord(domain, hostname, recordType string) error {
	c.mutex.Lock()
	key := fmt.Sprintf("%s:%s:%s", domain, hostname, recordType)

	// Check if record actually exists
	if _, exists := c.records[key]; !exists {
		c.mutex.Unlock()
		log.Debug("[output/dns/cloudflare] Record %s.%s (%s) does not exist, skipping removal", hostname, domain, recordType)
		return nil
	}

	// Remove the record immediately and sync only this deletion
	delete(c.records, key)
	c.mutex.Unlock()

	// Immediately sync only this deletion to prevent batching with other changes
	log.Debug("[output/dns/cloudflare] Immediately syncing deletion of: %s.%s %s", hostname, domain, recordType)
	return c.syncSingleDeletion(domain, hostname, recordType)
}

func (c *cloudflareFormat) Sync() error {
	// Prevent concurrent syncs to the same Cloudflare account
	c.syncMutex.Lock()
	defer c.syncMutex.Unlock()

	c.mutex.Lock()

	// Only sync records that have changed
	changedRecordKeys := make([]string, 0, len(c.changedRecords))
	for key := range c.changedRecords {
		changedRecordKeys = append(changedRecordKeys, key)
	}

	if len(changedRecordKeys) == 0 {
		c.mutex.Unlock()
		log.Debug("[output/dns/cloudflare] No changed records to sync")
		return nil
	}

	log.Info("[output/dns/cloudflare] Syncing %d changed record(s) to Cloudflare DNS", len(changedRecordKeys))

	// Actually sync the records instead of dry-run
	var errors []string
	for _, key := range changedRecordKeys {
		if record, exists := c.records[key]; exists {
			log.Debug("[output/dns/cloudflare] → Updating: %s.%s (%s) -> %s (TTL: %d)",
				record.Hostname, record.Domain, record.RecordType, record.Target, record.TTL)
			err := c.syncSingleRecord(record)
			if err != nil {
				errors = append(errors, fmt.Sprintf("record %s: %v", key, err))
				log.Error("[output/dns/cloudflare] Failed to sync record %s: %v", key, err)
			}
		} else {
			log.Debug("[output/dns/cloudflare] → Deleting: %s", key)
			// Handle deletion if needed
		}
	}

	// Clear changed records after sync
	c.changedRecords = make(map[string]bool)
	c.mutex.Unlock()

	if len(errors) > 0 {
		return fmt.Errorf("failed to sync some records: %s", strings.Join(errors, "; "))
	}

	log.Info("[output/dns/cloudflare] Sync completed successfully - %d record(s) processed", len(changedRecordKeys))
	return nil
}

// syncSingleDeletion immediately deletes a single DNS record without batching
func (c *cloudflareFormat) syncSingleDeletion(domain, hostname, recordType string) error {
	log.Debug("[output/dns/cloudflare] Syncing single deletion: %s.%s (%s)", hostname, domain, recordType)

	// Get zone ID
	zoneID, err := c.getZoneID(domain)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to get zone ID for domain %s: %v", domain, err)
		return fmt.Errorf("failed to get zone ID for domain %s: %v", domain, err)
	}

	// Find existing record to delete
	record := &dnsRecord{
		Domain:     domain,
		Hostname:   hostname,
		RecordType: recordType,
	}

	existingRecordID, err := c.findExistingRecord(zoneID, record)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to find record for deletion: %v", err)
		return fmt.Errorf("failed to find record for deletion: %v", err)
	}

	if existingRecordID == "" {
		log.Debug("[output/dns/cloudflare] Record %s.%s (%s) not found in DNS, already deleted", hostname, domain, recordType)
		return nil
	}

	// Delete the record
	return c.deleteRecord(zoneID, existingRecordID, hostname, domain, recordType)
}

// deleteRecord deletes a DNS record by ID
func (c *cloudflareFormat) deleteRecord(zoneID, recordID, hostname, domain, recordType string) error {
	recordName := hostname + "." + domain
	if hostname == "@" || hostname == "" {
		recordName = domain
	}

	log.Debug("[output/dns/cloudflare] Deleting DNS record ID %s: %s (%s)", recordID, recordName, recordType)

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, recordID)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to create HTTP request for record deletion: %v", err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error("[output/dns/cloudflare] HTTP request failed for record deletion: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Error("[output/dns/cloudflare] Delete record API error %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("failed to delete record: %d %s", resp.StatusCode, string(body))
	}

	log.Info("[output/dns/cloudflare] Deleted DNS record: %s %s", recordName, recordType)
	return nil
}

// syncSingleRecordWithZone syncs a single record using a pre-fetched zone ID
func (c *cloudflareFormat) syncSingleRecordWithZone(zoneID string, record *dnsRecord) error {
	log.Debug("[output/dns/cloudflare] Syncing record: %s.%s (%s) -> %s", record.Hostname, record.Domain, record.RecordType, record.Target)

	// Check if record already exists
	log.Trace("[output/dns/cloudflare] Checking for existing record: %s.%s (%s)", record.Hostname, record.Domain, record.RecordType)
	existingRecordID, err := c.findExistingRecord(zoneID, record)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to check existing record: %v", err)
		return fmt.Errorf("failed to check existing record: %v", err)
	}

	if existingRecordID != "" {
		log.Debug("[output/dns/cloudflare] Found existing record ID: %s, updating", existingRecordID)
		// Update existing record
		return c.updateRecord(zoneID, existingRecordID, record)
	} else {
		log.Debug("[output/dns/cloudflare] No existing record found, creating new record")
		// Create new record
		return c.createRecord(zoneID, record)
	}
}

func (c *cloudflareFormat) syncSingleRecord(record *dnsRecord) error {
	log.Debug("[output/dns/cloudflare] Syncing record: %s.%s (%s) -> %s", record.Hostname, record.Domain, record.RecordType, record.Target)

	// First, get the zone ID for the domain
	log.Trace("[output/dns/cloudflare] Getting zone ID for domain: %s", record.Domain)
	zoneID, err := c.getZoneID(record.Domain)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to get zone ID for domain %s: %v", record.Domain, err)
		return fmt.Errorf("failed to get zone ID for domain %s: %v", record.Domain, err)
	}
	log.Debug("[output/dns/cloudflare] Found zone ID: %s for domain: %s", zoneID, record.Domain)

	// Check if record already exists
	log.Trace("[output/dns/cloudflare] Checking for existing record: %s.%s (%s)", record.Hostname, record.Domain, record.RecordType)
	existingRecordID, err := c.findExistingRecord(zoneID, record)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to check existing record: %v", err)
		return fmt.Errorf("failed to check existing record: %v", err)
	}

	if existingRecordID != "" {
		log.Debug("[output/dns/cloudflare] Found existing record ID: %s, updating", existingRecordID)
		// Update existing record
		return c.updateRecord(zoneID, existingRecordID, record)
	} else {
		log.Debug("[output/dns/cloudflare] No existing record found, creating new record")
		// Create new record
		return c.createRecord(zoneID, record)
	}
}

func (c *cloudflareFormat) getZoneID(domain string) (string, error) {
	// Check cache first
	c.zoneMutex.RLock()
	if zoneID, exists := c.zoneCache[domain]; exists {
		c.zoneMutex.RUnlock()
		log.Debug("[output/dns/cloudflare] Using cached zone ID: %s for domain: %s", zoneID, domain)
		return zoneID, nil
	}
	c.zoneMutex.RUnlock()

	log.Trace("[output/dns/cloudflare] Making API call to get zone ID for domain: %s", domain)

	// Make API call to get zone ID
	url := "https://api.cloudflare.com/client/v4/zones?name=" + domain

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to create HTTP request for zone lookup: %v", err)
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	log.Trace("[output/dns/cloudflare] Sending GET request to: %s", url)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error("[output/dns/cloudflare] HTTP request failed for zone lookup: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Error("[output/dns/cloudflare] Zone lookup API error %d: %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("cloudflare API error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Success bool `json:"success"`
		Result  []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		log.Error("[output/dns/cloudflare] Failed to decode zone lookup response: %v", err)
		return "", err
	}

	log.Trace("[output/dns/cloudflare] Zone lookup response: success=%v, results_count=%d", response.Success, len(response.Result))

	if !response.Success || len(response.Result) == 0 {
		log.Error("[output/dns/cloudflare] Zone not found for domain %s", domain)
		return "", fmt.Errorf("zone not found for domain %s", domain)
	}

	zoneID := response.Result[0].ID

	// Cache the zone ID
	c.zoneMutex.Lock()
	c.zoneCache[domain] = zoneID
	c.zoneMutex.Unlock()

	log.Debug("[output/dns/cloudflare] Successfully found and cached zone ID: %s for domain: %s", zoneID, domain)
	return zoneID, nil
}

func (c *cloudflareFormat) findExistingRecord(zoneID string, record *dnsRecord) (string, error) {
	// Construct record name
	recordName := record.Hostname + "." + record.Domain
	if record.Hostname == "@" || record.Hostname == "" {
		recordName = record.Domain
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?name=%s&type=%s",
		zoneID, recordName, record.RecordType)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("cloudflare API error %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		Success bool `json:"success"`
		Result  []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}

	if response.Success && len(response.Result) > 0 {
		return response.Result[0].ID, nil
	}

	return "", nil // No existing record found
}

func (c *cloudflareFormat) createRecord(zoneID string, record *dnsRecord) error {
	recordName := record.Hostname + "." + record.Domain
	if record.Hostname == "@" || record.Hostname == "" {
		recordName = record.Domain
	}

	log.Debug("[output/dns/cloudflare] Creating new DNS record: %s (%s) -> %s", recordName, record.RecordType, record.Target)

	payload := map[string]interface{}{
		"type":    record.RecordType,
		"name":    recordName,
		"content": record.Target,
		"ttl":     record.TTL,
	}

	log.Trace("[output/dns/cloudflare] Create record payload: %+v", payload)

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to marshal create record payload: %v", err)
		return err
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records", zoneID)
	log.Trace("[output/dns/cloudflare] Sending POST request to: %s", url)

	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBytes)))
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to create HTTP request for record creation: %v", err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error("[output/dns/cloudflare] HTTP request failed for record creation: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Error("[output/dns/cloudflare] Create record API error %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("failed to create record: %d %s", resp.StatusCode, string(body))
	}

	log.Info("[output/dns/cloudflare] Created DNS record: %s %s -> %s", recordName, record.RecordType, record.Target)
	return nil
}

func (c *cloudflareFormat) updateRecord(zoneID, recordID string, record *dnsRecord) error {
	recordName := record.Hostname + "." + record.Domain
	if record.Hostname == "@" || record.Hostname == "" {
		recordName = record.Domain
	}

	log.Debug("[output/dns/cloudflare] Updating existing DNS record ID %s: %s (%s) -> %s", recordID, recordName, record.RecordType, record.Target)

	payload := map[string]interface{}{
		"type":    record.RecordType,
		"name":    recordName,
		"content": record.Target,
		"ttl":     record.TTL,
	}

	log.Trace("[output/dns/cloudflare] Update record payload: %+v", payload)

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to marshal update record payload: %v", err)
		return err
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, recordID)
	log.Trace("[output/dns/cloudflare] Sending PUT request to: %s", url)

	req, err := http.NewRequest("PUT", url, strings.NewReader(string(jsonBytes)))
	if err != nil {
		log.Error("[output/dns/cloudflare] Failed to create HTTP request for record update: %v", err)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error("[output/dns/cloudflare] HTTP request failed for record update: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Error("[output/dns/cloudflare] Update record API error %d: %s", resp.StatusCode, string(body))
		return fmt.Errorf("failed to update record: %d %s", resp.StatusCode, string(body))
	}

	log.Info("[output/dns/cloudflare] Updated DNS record: %s %s -> %s", recordName, record.RecordType, record.Target)
	return nil
}
