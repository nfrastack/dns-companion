// SPDX-FileCopyrightText: © 2025 Nfrastack <code@nfrastack.com>
//
// SPDX-License-Identifier: BSD-3-Clause

package providers

import (
	"herald/pkg/log"
	"herald/pkg/output/types/dns"
	"herald/pkg/util"

	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go"
)

func init() {
	dns.RegisterProvider("cloudflare", NewCloudflareProvider)
}

// CloudflareProvider implements the DNS provider interface for Cloudflare
type CloudflareProvider struct {
	client  *cloudflare.API
	config  map[string]string
	logger  *log.ScopedLogger
	retries int
	timeout time.Duration
}

// NewCloudflareProvider creates a new Cloudflare DNS provider
func NewCloudflareProvider(config map[string]string) (dns.Provider, error) {
	token, ok := config["token"]
	if !ok || token == "" {
		// Try legacy fields as fallbacks
		if apiToken, exists := config["api_token"]; exists {
			token = apiToken
		} else {
			return nil, fmt.Errorf("cloudflare provider requires 'token' or 'api_token' parameter")
		}
	}

	// Support file:// and env:// references for the token
	resolvedToken := util.ReadSecretValue(token)
	if resolvedToken == "" {
		return nil, fmt.Errorf("cloudflare provider token is empty after resolution")
	}

	// Create Cloudflare client
	api, err := cloudflare.NewWithAPIToken(resolvedToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloudflare client: %v", err)
	}

	// Parse optional configuration
	retries := 3
	if retriesStr, ok := config["retries"]; ok && retriesStr != "" {
		if r, err := strconv.Atoi(retriesStr); err == nil && r > 0 {
			retries = r
		}
	}

	timeout := 30 * time.Second
	if timeoutStr, ok := config["timeout"]; ok && timeoutStr != "" {
		if t, err := time.ParseDuration(timeoutStr); err == nil && t > 0 {
			timeout = t
		}
	}

	// Create scoped logger
	logLevel := config["log_level"]
	logger := log.NewScopedLogger("[output/dns/cloudflare]", logLevel)

	provider := &CloudflareProvider{
		client:  api,
		config:  config,
		logger:  logger,
		retries: retries,
		timeout: timeout,
	}

	logger.Info("Cloudflare DNS provider initialized (retries: %d, timeout: %v)", retries, timeout)
	return provider, nil
}

// CreateOrUpdateRecord creates or updates a DNS record
func (c *CloudflareProvider) CreateOrUpdateRecord(domain, recordType, name, target string, ttl int, proxied bool) error {
	return c.CreateOrUpdateRecordWithSource(domain, recordType, name, target, ttl, proxied, "", "herald")
}

// CreateOrUpdateRecordWithSource creates or updates a DNS record with source information
func (c *CloudflareProvider) CreateOrUpdateRecordWithSource(domain, recordType, name, target string, ttl int, proxied bool, comment, source string) error {
	c.logger.Debug("Creating/updating record: %s.%s %s -> %s (TTL: %d, Proxied: %t)", name, domain, recordType, target, ttl, proxied)

	ctx := context.Background()

	// Get zone ID
	zoneID, err := c.getZoneID(ctx, domain)
	if err != nil {
		return fmt.Errorf("failed to get zone ID for domain %s: %v", domain, err)
	}

	// Format the full hostname
	fullName := name
	if name != "@" && name != "" && !strings.HasSuffix(name, "."+domain) {
		fullName = name + "." + domain
	} else if name == "@" {
		fullName = domain
	}

	// Look for existing record
	existingRecord, err := c.findExistingRecord(ctx, zoneID, fullName, recordType)
	if err != nil {
		return fmt.Errorf("failed to search for existing record: %v", err)
	}

	// Prepare record data
	recordParams := cloudflare.CreateDNSRecordParams{
		Type:    recordType,
		Name:    fullName,
		Content: target,
		TTL:     ttl,
		Proxied: &proxied,
	}

	// Add comment if provided (comments are not supported in CreateDNSRecordParams)
	// The comment field is only available in some Cloudflare API operations

	// Create resource container for the zone
	rc := cloudflare.ZoneIdentifier(zoneID)

	// Update or create record
	if existingRecord != nil {
		c.logger.Debug("Updating existing record %s", existingRecord.ID)

		updateParams := cloudflare.UpdateDNSRecordParams{
			Type:    recordType,
			Name:    fullName,
			Content: target,
			TTL:     ttl,
			Proxied: &proxied,
		}

		if comment != "" {
			updateParams.Comment = &comment
		}

		_, err = c.client.UpdateDNSRecord(ctx, rc, updateParams)
		if err != nil {
			return fmt.Errorf("failed to update DNS record: %v", err)
		}

		c.logger.Info("Updated DNS record: %s %s -> %s", fullName, recordType, target)
	} else {
		c.logger.Debug("Creating new record")

		_, err = c.client.CreateDNSRecord(ctx, rc, recordParams)
		if err != nil {
			return fmt.Errorf("failed to create DNS record: %v", err)
		}

		c.logger.Info("Created DNS record: %s %s -> %s", fullName, recordType, target)
	}

	return nil
}

// DeleteRecord deletes a DNS record
func (c *CloudflareProvider) DeleteRecord(domain, recordType, name string) error {
	c.logger.Debug("Deleting record: %s.%s %s", name, domain, recordType)

	ctx := context.Background()

	// Get zone ID
	zoneID, err := c.getZoneID(ctx, domain)
	if err != nil {
		return fmt.Errorf("failed to get zone ID for domain %s: %v", domain, err)
	}

	// Format the full hostname
	fullName := name
	if name != "@" && name != "" && !strings.HasSuffix(name, "."+domain) {
		fullName = name + "." + domain
	} else if name == "@" {
		fullName = domain
	}

	// Find the record to delete
	existingRecord, err := c.findExistingRecord(ctx, zoneID, fullName, recordType)
	if err != nil {
		return fmt.Errorf("failed to search for existing record: %v", err)
	}

	if existingRecord == nil {
		c.logger.Warn("Record not found for deletion: %s %s", fullName, recordType)
		return nil // Not an error - record doesn't exist
	}

	// Create resource container for the zone
	rc := cloudflare.ZoneIdentifier(zoneID)

	// Delete the record
	err = c.client.DeleteDNSRecord(ctx, rc, existingRecord.ID)
	if err != nil {
		return fmt.Errorf("failed to delete DNS record: %v", err)
	}

	c.logger.Info("Deleted DNS record: %s %s", fullName, recordType)
	return nil
}

// getZoneID retrieves the zone ID for a domain
func (c *CloudflareProvider) getZoneID(ctx context.Context, domain string) (string, error) {
	// Try to find the zone
	zones, err := c.client.ListZones(ctx, domain)
	if err != nil {
		return "", fmt.Errorf("failed to list zones: %v", err)
	}

	if len(zones) == 0 {
		return "", fmt.Errorf("no zone found for domain: %s", domain)
	}

	// Return the first matching zone
	return zones[0].ID, nil
}

// findExistingRecord searches for an existing DNS record
func (c *CloudflareProvider) findExistingRecord(ctx context.Context, zoneID, name, recordType string) (*cloudflare.DNSRecord, error) {
	// Create resource container for the zone
	rc := cloudflare.ZoneIdentifier(zoneID)

	// Search for records with matching name and type
	params := cloudflare.ListDNSRecordsParams{
		Name: name,
		Type: recordType,
	}

	records, _, err := c.client.ListDNSRecords(ctx, rc, params)
	if err != nil {
		return nil, fmt.Errorf("failed to search DNS records: %v", err)
	}

	if len(records) == 0 {
		return nil, nil // No existing record found
	}

	// Return the first matching record
	return &records[0], nil
}

// GetName returns the provider name
func (c *CloudflareProvider) GetName() string {
	return "cloudflare"
}

// Validate validates the provider configuration
func (c *CloudflareProvider) Validate() error {
	if c.client == nil {
		return fmt.Errorf("cloudflare client not initialized")
	}

	ctx := context.Background()

	// Test the connection by listing zones (limited to 1 for efficiency)
	_, err := c.client.ListZones(ctx)
	if err != nil {
		return fmt.Errorf("failed to validate Cloudflare connection: %v", err)
	}

	c.logger.Debug("Cloudflare provider validation successful")
	return nil
}
