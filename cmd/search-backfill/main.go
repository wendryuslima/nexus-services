// search-backfill builds search projections for existing data. New writes already
// contain their words. Run after deploying the API and before enabling search.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	chatrepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/chat_repository"
	timelinerepository "github.com/wendryuslima/nexus-services/internal/resources/mongodb/timeline_repository"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	flags := flag.NewFlagSet("search-backfill", flag.ContinueOnError)
	envFile := flags.String("env-file", ".env", "Arquivo de configuração; variáveis do ambiente têm prioridade")
	address := flags.String("mongo-address", "", "Endereço direto do MongoDB, por exemplo 127.0.0.1:27019; preserva as credenciais da URI")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *envFile != "" {
		if err := godotenv.Load(*envFile); err != nil && !(errors.Is(err, os.ErrNotExist) && *envFile == ".env") {
			return fmt.Errorf("não foi possível carregar o arquivo de ambiente")
		}
	}
	uri, database := os.Getenv("MONGODB_URI"), os.Getenv("MONGODB_DATABASE")
	if uri == "" || database == "" {
		return fmt.Errorf("MONGODB_URI and MONGODB_DATABASE are required")
	}
	if *address != "" {
		var err error
		uri, err = uriAtAddress(uri, *address)
		if err != nil {
			return err
		}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetConnectTimeout(10 * time.Second).SetServerSelectionTimeout(10 * time.Second))
	if err != nil {
		return fmt.Errorf("connect MongoDB: %w", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = client.Disconnect(cleanup)
	}()
	chats, err := chatrepository.NewRepository(client.Database(database).Collection("chats"))
	if err != nil {
		return err
	}
	timeline, err := timelinerepository.NewRepository(client.Database(database).Collection("timeline_items"))
	if err != nil {
		return err
	}
	if err := chats.EnsureIndexes(ctx); err != nil {
		return err
	}
	if err := timeline.EnsureIndexes(ctx); err != nil {
		return err
	}
	if err := chats.BackfillSearchWords(ctx); err != nil {
		return fmt.Errorf("backfill conversations: %w", err)
	}
	if err := timeline.BackfillSearchWords(ctx); err != nil {
		return fmt.Errorf("backfill messages: %w", err)
	}
	log.Print("Search projections are up to date")
	return nil
}

// A direct address is useful outside Docker, where replica-set member names
// from the Compose network are not resolvable. Credentials stay in the env file.
func uriAtAddress(uri, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return "", fmt.Errorf("mongo-address deve ter o formato host:porta")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("porta de mongo-address inválida")
	}
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "mongodb" || parsed.Host == "" {
		return "", fmt.Errorf("mongo-address requer uma MONGODB_URI válida com esquema mongodb://")
	}
	parsed.Host = net.JoinHostPort(host, port)
	query := parsed.Query()
	query.Set("directConnection", "true")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
