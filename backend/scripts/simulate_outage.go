//go:build ignore

// simulate_outage.go sends a scripted cascading failure sequence to the IMS ingest API.
// Usage: go run scripts/simulate_outage.go [--addr http://localhost:8080] [--token <JWT>]
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"
)

var (
	addr  = flag.String("addr", "http://localhost:8080", "IMS backend address")
	token = flag.String("token", "", "JWT bearer token (producer role)")
)

type Signal struct {
	ComponentID   string            `json:"component_id"`
	ComponentType string            `json:"component_type"`
	Severity      string            `json:"severity"`
	Message       string            `json:"message"`
	Tags          map[string]string `json:"tags,omitempty"`
	Timestamp     string            `json:"timestamp"`
}

func main() {
	flag.Parse()

	if *token == "" {
		// Auto-login as producer.
		t, err := login()
		if err != nil {
			log.Fatalf("login failed: %v — start the backend first", err)
		}
		*token = t
		log.Printf("Logged in as producer")
	}

	now := time.Now()

	// t+0s: RDBMS outage — 200 errors → P0 work item
	log.Println("t+0s: Injecting RDBMS failure (200 signals)...")
	send(make200("RDBMS_PRIMARY", "RDBMS", "connection refused: max connections exceeded", now))
	time.Sleep(2 * time.Second)

	// t+2s: MCP_HOST cascade — 500 timeouts → P1 work item
	log.Println("t+2s: Injecting MCP_HOST cascade (500 signals)...")
	for i := 0; i < 5; i++ {
		send(make100(fmt.Sprintf("MCP_HOST_%02d", i+1), "MCP_HOST", "upstream timeout waiting for RDBMS", now.Add(2*time.Second)))
	}
	time.Sleep(3 * time.Second)

	// t+5s: Cache evictions — 1000 events → debounced into one P2 work item
	log.Println("t+5s: Injecting CACHE eviction storm (1000 signals, debounced)...")
	for i := 0; i < 10; i++ {
		send(make100("CACHE_CLUSTER_01", "CACHE", "LRU eviction: cache full due to RDBMS backlog", now.Add(5*time.Second)))
	}
	time.Sleep(3 * time.Second)

	// t+8s: Queue depth spike
	log.Println("t+8s: Injecting ASYNC_QUEUE backlog (50 signals)...")
	send(make50("ASYNC_QUEUE_01", "ASYNC_QUEUE", "consumer lag spike: queue depth > 100k", now.Add(8*time.Second)))
	time.Sleep(2 * time.Second)

	// t+10s: NoSQL secondary fails
	log.Println("t+10s: Injecting NoSQL secondary failure (50 signals)...")
	send(make50("NOSQL_REPLICA_01", "NOSQL", "replica lag > 30s: promoting secondary", now.Add(10*time.Second)))
	time.Sleep(2 * time.Second)

	// t+12s: Recovery signals
	log.Println("t+12s: Injecting recovery signals...")
	send([]Signal{
		{ComponentID: "RDBMS_PRIMARY", ComponentType: "RDBMS", Severity: "P3", Message: "connections stabilising", Timestamp: now.Add(12 * time.Second).Format(time.RFC3339)},
		{ComponentID: "MCP_HOST_01", ComponentType: "MCP_HOST", Severity: "P3", Message: "upstream latency normalising", Timestamp: now.Add(13 * time.Second).Format(time.RFC3339)},
		{ComponentID: "CACHE_CLUSTER_01", ComponentType: "CACHE", Severity: "P3", Message: "cache hit rate recovering", Timestamp: now.Add(14 * time.Second).Format(time.RFC3339)},
	})

	log.Println("Simulation complete. Check the dashboard at http://localhost:3000")
	log.Println("Use the UI to transition incidents through INVESTIGATING → RESOLVED → (submit RCA) → CLOSED")
}

func login() (string, error) {
	body, _ := json.Marshal(map[string]string{"email": "producer@ims.local", "password": "password123"})
	resp, err := http.Post(*addr+"/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var pair struct{ AccessToken string `json:"access_token"` }
	if err := json.NewDecoder(resp.Body).Decode(&pair); err != nil {
		return "", err
	}
	return pair.AccessToken, nil
}

func send(signals []Signal) {
	body, _ := json.Marshal(map[string]interface{}{"signals": signals})
	req, _ := http.NewRequest(http.MethodPost, *addr+"/api/v1/signals", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+*token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("send error: %v", err)
		return
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	log.Printf("  → HTTP %d accepted=%v dropped=%v", resp.StatusCode, result["accepted"], result["dropped"])
}

func make200(id, typ, msg string, ts time.Time) []Signal {
	sigs := make([]Signal, 200)
	for i := range sigs {
		sigs[i] = Signal{ComponentID: id, ComponentType: typ, Severity: "P0", Message: msg,
			Tags: map[string]string{"attempt": fmt.Sprintf("%d", i)},
			Timestamp: ts.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)}
	}
	return sigs
}

func make100(id, typ, msg string, ts time.Time) []Signal {
	sigs := make([]Signal, 100)
	for i := range sigs {
		sigs[i] = Signal{ComponentID: id, ComponentType: typ, Severity: "P1", Message: msg,
			Timestamp: ts.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)}
	}
	return sigs
}

func make50(id, typ, msg string, ts time.Time) []Signal {
	sigs := make([]Signal, 50)
	for i := range sigs {
		sigs[i] = Signal{ComponentID: id, ComponentType: typ, Severity: "P2", Message: msg,
			Timestamp: ts.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)}
	}
	return sigs
}
