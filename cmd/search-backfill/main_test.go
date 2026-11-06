package main

import (
	"net/url"
	"testing"
)

func TestLocalAddressPreservesCredentialsDatabaseAndOptions(t *testing.T) {
	original := "mongodb://example:p%40ss@mongodb:27017/nexus?authSource=admin&replicaSet=rs0"
	value, err := uriAtAddress(original, "127.0.0.1:27019")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	if parsed.Host != "127.0.0.1:27019" || parsed.User.Username() != "example" || password != "p@ss" || parsed.Path != "/nexus" {
		t.Fatal("connection identity changed")
	}
	if parsed.Query().Get("authSource") != "admin" || parsed.Query().Get("replicaSet") != "rs0" || parsed.Query().Get("directConnection") != "true" {
		t.Fatal("connection options were not preserved")
	}
}

func TestInvalidAddressDoesNotExposeCredentials(t *testing.T) {
	for _, address := range []string{"localhost", "localhost:0", "localhost:65536", "localhost:abc", ":27019"} {
		if _, err := uriAtAddress("mongodb://example:secret@mongodb:27017", address); err == nil {
			t.Fatalf("accepted %s", address)
		}
	}
	if _, err := uriAtAddress("mongodb+srv://example:secret@cluster.example.com", "localhost:27019"); err == nil {
		t.Fatal("accepted a direct address for SRV")
	}
}
