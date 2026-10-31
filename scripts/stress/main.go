package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type configuration struct {
	baseURL         string
	origin          string
	users           int
	duration        time.Duration
	interval        time.Duration
	requestTimeout  time.Duration
	setupTimeout    time.Duration
	setupWorkers    int
	messageSize     int
	ignoreRateLimit bool
	output          string
}

type virtualUser struct {
	client *http.Client
	id     string
	chatID string
}

func main() {
	config := configuration{}
	flag.StringVar(&config.baseURL, "base-url", "http://localhost:8080", "URL da API")
	flag.StringVar(&config.origin, "origin", "", "Origin permitido pela API, se necessário")
	flag.IntVar(&config.users, "users", 100, "Quantidade de usuários simultâneos (mínimo 2)")
	flag.DurationVar(&config.duration, "duration", 2*time.Minute, "Duração da fase de envio, excluindo preparação")
	flag.DurationVar(&config.interval, "interval", 1100*time.Millisecond, "Intervalo mínimo entre inícios de envios de cada usuário; 0 envia sem pausa")
	flag.DurationVar(&config.requestTimeout, "request-timeout", 15*time.Second, "Timeout por requisição")
	flag.DurationVar(&config.setupTimeout, "setup-timeout", 5*time.Minute, "Timeout total da preparação")
	flag.IntVar(&config.setupWorkers, "setup-workers", 4, "Cadastros/logins simultâneos na preparação")
	flag.IntVar(&config.messageSize, "message-size", 256, "Tamanho do texto em caracteres ASCII (64 a 4000)")
	flag.BoolVar(&config.ignoreRateLimit, "ignore-rate-limit", false, "Continuar enviando após 429 sem aguardar Retry-After")
	flag.StringVar(&config.output, "output", "", "Arquivo JSON de resultados; padrão stress-results/<execução>.json")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := execute(ctx, config, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (config configuration) validate() error {
	parsed, err := url.Parse(config.baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("base-url deve ser uma URL HTTP(S), sem credenciais, query ou fragmento")
	}
	if config.users < 2 || config.setupWorkers < 1 || config.duration <= 0 || config.interval < 0 || config.setupTimeout <= 0 {
		return fmt.Errorf("users >= 2, setup-workers >= 1, duration e setup-timeout positivos e interval >= 0 são obrigatórios")
	}
	if config.requestTimeout <= 0 || config.requestTimeout > time.Minute {
		return fmt.Errorf("request-timeout deve estar entre zero e 1m")
	}
	if config.messageSize < 64 || config.messageSize > 4000 {
		return fmt.Errorf("message-size deve estar entre 64 e 4000")
	}
	return nil
}

func execute(ctx context.Context, config configuration, out io.Writer) error {
	if err := config.validate(); err != nil {
		return err
	}
	config.baseURL = strings.TrimRight(config.baseURL, "/")
	runID := time.Now().UTC().Format("20060102T150405Z") + "-" + strings.ToLower(rand.Text()[:8])
	if config.output == "" {
		config.output = filepath.Join("stress-results", runID+".json")
	}
	if err := os.MkdirAll(filepath.Dir(config.output), 0755); err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = config.users * 2
	transport.MaxIdleConnsPerHost = config.users * 2
	transport.MaxConnsPerHost = config.users * 2
	defer transport.CloseIdleConnections()
	users := make([]virtualUser, config.users)
	for i := range users {
		jar, err := cookiejar.New(nil)
		if err != nil {
			return err
		}
		users[i].client = &http.Client{Transport: transport, Jar: jar, Timeout: config.requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	fmt.Fprintf(out, "Execução %s | API %s | %d usuários\n", runID, config.baseURL, config.users)
	fmt.Fprintf(out, "Preparando contas e sessões (%d por vez); esta etapa grava dados de teste.\n", config.setupWorkers)
	setupStart := time.Now()
	setupCtx, stopSetup := context.WithTimeout(ctx, config.setupTimeout)
	err := prepare(setupCtx, config, users, runID)
	stopSetup()
	if err != nil {
		return fmt.Errorf("preparação interrompida: %w", err)
	}
	setupElapsed := time.Since(setupStart)
	fmt.Fprintf(out, "Preparação concluída em %s. Enviando por %s, intervalo %s por usuário.\n", setupElapsed.Round(time.Millisecond), config.duration, config.interval)
	stats := newStatistics(config.requestTimeout)
	started := time.Now()
	runLoad(ctx, config, users, runID, stats, out)
	result := stats.report(runID, config, started, setupElapsed, time.Since(started), ctx.Err() != nil)
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(config.output, append(raw, '\n'), 0600); err != nil {
		return fmt.Errorf("gravar relatório: %w", err)
	}
	fmt.Fprintf(out, "\nTentativas: %d | Criadas: %d | Retries confirmados: %d | 429: %d | Falhas: %d\n", result.Attempts, result.Created, result.Idempotent, result.RateLimited, result.Failures)
	fmt.Fprintf(out, "Vazão: %.2f tentativas/s | %.2f mensagens criadas/s\n", result.AttemptsPerSecond, result.CreatedPerSecond)
	fmt.Fprintf(out, "Latência de envios persistidos (ms): p50=%.0f p95=%.0f p99=%.0f máximo=%.2f\n", result.PersistedLatency.P50MS, result.PersistedLatency.P95MS, result.PersistedLatency.P99MS, result.PersistedLatency.MaxMS)
	fmt.Fprintf(out, "Relatório: %s\n", config.output)
	if ctx.Err() != nil {
		return fmt.Errorf("teste interrompido; relatório parcial salvo")
	}
	if result.Failures > 0 {
		return fmt.Errorf("teste registrou %d falhas inesperadas; consulte o relatório", result.Failures)
	}
	return nil
}

func prepare(ctx context.Context, config configuration, users []virtualUser, runID string) error {
	password := rand.Text() + ".Aa1"
	if err := parallel(ctx, len(users), config.setupWorkers, func(ctx context.Context, i int) error {
		credentials := map[string]string{"email": fmt.Sprintf("stress-%s-%03d@example.test", strings.ToLower(runID), i), "password": password}
		response := sendJSON(ctx, users[i].client, config, "/v1/auth/signup", credentials)
		if err := response.expect(http.StatusCreated); err != nil {
			return fmt.Errorf("cadastro do usuário %d: %w", i, err)
		}
		response = sendJSON(ctx, users[i].client, config, "/v1/auth/signin", credentials)
		if err := response.expect(http.StatusOK); err != nil {
			return fmt.Errorf("login do usuário %d: %w", i, err)
		}
		var payload struct {
			Data struct {
				UserID string `json:"user_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.body, &payload); err != nil || payload.Data.UserID == "" {
			return fmt.Errorf("login do usuário %d retornou resposta inválida", i)
		}
		users[i].id = payload.Data.UserID
		return nil
	}); err != nil {
		return err
	}
	return parallel(ctx, len(users), config.setupWorkers, func(ctx context.Context, i int) error {
		peer := i ^ 1
		if peer >= len(users) {
			peer = 0
		}
		response := sendJSON(ctx, users[i].client, config, "/v1/chats/direct", map[string]string{"relatedUserId": users[peer].id})
		if err := response.expect(http.StatusOK, http.StatusCreated); err != nil {
			return fmt.Errorf("chat do usuário %d: %w", i, err)
		}
		var payload struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.body, &payload); err != nil || payload.Data.ID == "" {
			return fmt.Errorf("chat do usuário %d retornou resposta inválida", i)
		}
		users[i].chatID = payload.Data.ID
		return nil
	})
}

func parallel(ctx context.Context, count, workers int, action func(context.Context, int) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	jobs := make(chan int)
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := action(ctx, i); err != nil {
					cancel(err)
					return
				}
			}
		})
	}
	for i := range count {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			group.Wait()
			return context.Cause(ctx)
		}
	}
	close(jobs)
	group.Wait()
	return context.Cause(ctx)
}

func runLoad(ctx context.Context, config configuration, users []virtualUser, runID string, stats *statistics, out io.Writer) {
	scheduleCtx, cancel := context.WithTimeout(ctx, config.duration)
	defer cancel()
	start := make(chan struct{})
	var ready, group sync.WaitGroup
	ready.Add(len(users))
	for i := range users {
		group.Go(func() {
			ready.Done()
			<-start
			for sequence := 1; scheduleCtx.Err() == nil; sequence++ {
				id := newMessageID()
				prefix := fmt.Sprintf("stress:%s user:%d message:%d ", runID, i, sequence)
				value := (prefix + strings.Repeat("x", config.messageSize))[:config.messageSize]
				payload := map[string]any{"clientMessageId": id, "content": map[string]string{"type": "TEXT", "value": value}}
				began := time.Now()
				response := sendJSON(ctx, users[i].client, config, "/v1/chats/"+url.PathEscape(users[i].chatID)+"/messages", payload)
				stats.record(response, time.Since(began), id, users[i].chatID)
				pause := max(time.Duration(0), config.interval-time.Since(began))
				if response.status == http.StatusTooManyRequests && !config.ignoreRateLimit {
					pause = max(pause, retryDelay(response.retryAfter, time.Now()))
				}
				if !waitFor(scheduleCtx, pause) {
					return
				}
			}
		})
	}
	ready.Wait()
	close(start)
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			stats.mu.Lock()
			fmt.Fprintf(out, "Tentativas=%d criadas=%d 429=%d falhas=%d\n", stats.attempts, stats.created, stats.rateLimited, stats.failures)
			stats.mu.Unlock()
		}
	}
}

func newMessageID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:])
}

func waitFor(ctx context.Context, pause time.Duration) bool {
	if pause <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}
