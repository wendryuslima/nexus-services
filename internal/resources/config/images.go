package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type ImagesConfig struct {
	Endpoint, PublicEndpoint, Region, Bucket, AccessKeyID, SecretAccessKey string
}

func LoadImagesConfig() (ImagesConfig, error) {
	c := ImagesConfig{Endpoint: os.Getenv("GARAGE_ENDPOINT"), PublicEndpoint: os.Getenv("GARAGE_PUBLIC_ENDPOINT"), Region: os.Getenv("GARAGE_REGION"), Bucket: os.Getenv("GARAGE_BUCKET"), AccessKeyID: os.Getenv("GARAGE_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("GARAGE_SECRET_ACCESS_KEY")}
	if c.Region == "" {
		c.Region = "garage"
	}
	if c.Bucket == "" {
		c.Bucket = "nexus-images"
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

func (c ImagesConfig) Validate() error {
	for name, value := range map[string]string{"GARAGE_ENDPOINT": c.Endpoint, "GARAGE_PUBLIC_ENDPOINT": c.PublicEndpoint} {
		u, err := url.Parse(value)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("%s must be an HTTP(S) origin", name)
		}
		if os.Getenv("APP_ENV") == "production" && u.Scheme != "https" && name == "GARAGE_PUBLIC_ENDPOINT" {
			return fmt.Errorf("GARAGE_PUBLIC_ENDPOINT must use HTTPS in production")
		}
	}
	for name, value := range map[string]string{"GARAGE_ACCESS_KEY_ID": c.AccessKeyID, "GARAGE_SECRET_ACCESS_KEY": c.SecretAccessKey, "GARAGE_REGION": c.Region, "GARAGE_BUCKET": c.Bucket} {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) {
			return fmt.Errorf("%s is required and must not have surrounding whitespace", name)
		}
	}
	return nil
}
