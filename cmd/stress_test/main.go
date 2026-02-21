// PingMe Stress Testing Tool
// Day 12: Concurrency stress testing and bottleneck analysis
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

var (
	serverURL   = flag.String("server", "http://127.0.0.1:8080", "Server base URL")
	wsURL       = flag.String("ws", "ws://127.0.0.1:8080/ws", "WebSocket URL")
	concurrency = flag.Int("c", 10, "Number of concurrent connections")
	duration    = flag.Int("d", 30, "Test duration in seconds")
	msgSize     = flag.Int("msg-size", 100, "Message size in bytes")
)

type TestMetrics struct {
	Connections    int64 `json:"connections"`
	TotalMsgs      int64 `json:"total_msgs"`
	FailedMsgs     int64 `json:"failed_msgs"`
	TotalLatency   int64 `json:"total_latency_ns"`
	MinLatency     int64 `json:"min_latency_ns"`
	MaxLatency     int64 `json:"max_latency_ns"`
	HTTP200        int64 `json:"http_200"`
	HTTPError      int64 `json:"http_error"`
	WSConnected    int64 `json:"ws_connected"`
	WSClosed       int64 `json:"ws_closed"`
	WSMessagesSent int64 `json:"ws_messages_sent"`
}

type LoginResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Token string `json:"token"`
		User  struct {
			UserID string `json:"user_id"`
		} `json:"user"`
	} `json:"data"`
}

var metrics TestMetrics
var stopCh chan struct{}

func main() {
	flag.Parse()
	
	fmt.Println("========================================")
	fmt.Println("PingMe Stress Testing Tool")
	fmt.Println("========================================")
	fmt.Printf("Server: %s\n", *serverURL)
	fmt.Printf("Concurrency: %d\n", *concurrency)
	fmt.Printf("Duration: %d seconds\n", *duration)
	fmt.Printf("Message Size: %d bytes\n", *msgSize)
	fmt.Println("========================================")

	stopCh = make(chan struct{})
	
	// Handle interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\n\nStopping stress test...")
		close(stopCh)
	}()

	// Run tests
	var wg sync.WaitGroup
	
	// Test 1: HTTP Login Stress Test
	fmt.Println("\n[1/4] Running HTTP Login Stress Test...")
	wg.Add(1)
	go runHTTPLoginTest(&wg)
	
	// Test 2: WebSocket Connection Test
	fmt.Println("\n[2/4] Running WebSocket Connection Test...")
	wg.Add(1)
	go runWebSocketTest(&wg)
	
	// Test 3: Message Send Test
	fmt.Println("\n[3/4] Running Message Send Test...")
	wg.Add(1)
	go runMessageSendTest(&wg)
	
	// Test 4: Historical Message Query Test
	fmt.Println("\n[4/4] Running Historical Message Query Test...")
	wg.Add(1)
	go runHistoryQueryTest(&wg)
	
	// Wait for duration or interrupt
	select {
	case <-stopCh:
	case <-time.After(time.Duration(*duration) * time.Second):
		close(stopCh)
	}
	
	wg.Wait()
	
	// Print final metrics
	printMetrics()
}

func runHTTPLoginTest(wg *sync.WaitGroup) {
	defer wg.Done()
	
	client := &http.Client{Timeout: 10 * time.Second}
	username := fmt.Sprintf("testuser%d", time.Now().Unix()%20+1)
	
	jsonStr := []byte(fmt.Sprintf(`{"username":"%s","password":"password123"}`, username))
	
	for {
		select {
		case <-stopCh:
			return
		default:
			start := time.Now()
			resp, err := client.Post(*serverURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(jsonStr))
			latency := time.Since(start).Nanoseconds()
			
			atomic.AddInt64(&metrics.TotalLatency, latency)
			atomic.AddInt64(&metrics.TotalMsgs, 1)
			
			if metrics.MinLatency == 0 || latency < atomic.LoadInt64(&metrics.MinLatency) {
				atomic.StoreInt64(&metrics.MinLatency, latency)
			}
			if latency > atomic.LoadInt64(&metrics.MaxLatency) {
				atomic.StoreInt64(&metrics.MaxLatency, latency)
			}
			
			if err != nil || resp.StatusCode != 200 {
				atomic.AddInt64(&metrics.HTTPError, 1)
			} else {
				atomic.AddInt64(&metrics.HTTP200, 1)
			}
			resp.Body.Close()
			
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func runWebSocketTest(wg *sync.WaitGroup) {
	defer wg.Done()
	
	var wgConn sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wgConn.Add(1)
		go func() {
			defer wgConn.Done()
			runSingleWSConnection()
		}()
	}
	wgConn.Wait()
}

func runSingleWSConnection() {
	// First login to get token
	client := &http.Client{Timeout: 10 * time.Second}
	username := fmt.Sprintf("testuser%d", time.Now().Unix()%20+1)
	
	jsonStr := []byte(fmt.Sprintf(`{"username":"%s","password":"password123"}`, username))
	resp, err := client.Post(*serverURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(jsonStr))
	if err != nil {
		return
	}
	
	var loginResp LoginResponse
	json.NewDecoder(resp.Body).Decode(&loginResp)
	resp.Body.Close()
	
	if loginResp.Code != 0 {
		return
	}
	
	token := loginResp.Data.Token
	userID := loginResp.Data.User.UserID
	
	// Connect to WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(*wsURL+"?token="+token+"&user_id="+userID, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	
	atomic.AddInt64(&metrics.WSConnected, 1)
	
	// Send messages
	msg := fmt.Sprintf("%0*s", *msgSize, "")
	for {
		select {
		case <-stopCh:
			atomic.AddInt64(&metrics.WSClosed, 1)
			return
		default:
			start := time.Now()
			err := ws.WriteMessage(websocket.TextMessage, []byte(msg))
			latency := time.Since(start).Nanoseconds()
			
			if err != nil {
				atomic.AddInt64(&metrics.WSClosed, 1)
				return
			}
			
			atomic.AddInt64(&metrics.WSMessagesSent, 1)
			atomic.AddInt64(&metrics.TotalMsgs, 1)
			atomic.AddInt64(&metrics.TotalLatency, latency)
			
			if metrics.MinLatency == 0 || latency < atomic.LoadInt64(&metrics.MinLatency) {
				atomic.StoreInt64(&metrics.MinLatency, latency)
			}
			if latency > atomic.LoadInt64(&metrics.MaxLatency) {
				atomic.StoreInt64(&metrics.MaxLatency, latency)
			}
			
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func runMessageSendTest(wg *sync.WaitGroup) {
	defer wg.Done()
	
	client := &http.Client{Timeout: 10 * time.Second}
	username := fmt.Sprintf("testuser%d", time.Now().Unix()%20+1)
	
	// Login first
	jsonStr := []byte(fmt.Sprintf(`{"username":"%s","password":"password123"}`, username))
	resp, err := client.Post(*serverURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(jsonStr))
	if err != nil {
		return
	}
	
	var loginResp LoginResponse
	json.NewDecoder(resp.Body).Decode(&loginResp)
	resp.Body.Close()
	
	token := loginResp.Data.Token
	
	_ = token // used in request
	
	toUser := fmt.Sprintf("testuser%d", (time.Now().Unix()%20+1)%20+1)
	_ = toUser // used in message body
	
	req, _ := http.NewRequest("POST", *serverURL+"/api/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	
	for {
		select {
		case <-stopCh:
			return
		default:
			start := time.Now()
			resp, err := client.Do(req)
			latency := time.Since(start).Nanoseconds()
			
			atomic.AddInt64(&metrics.TotalLatency, latency)
			atomic.AddInt64(&metrics.TotalMsgs, 1)
			
			if metrics.MinLatency == 0 || latency < atomic.LoadInt64(&metrics.MinLatency) {
				atomic.StoreInt64(&metrics.MinLatency, latency)
			}
			if latency > atomic.LoadInt64(&metrics.MaxLatency) {
				atomic.StoreInt64(&metrics.MaxLatency, latency)
			}
			
			if err != nil || resp.StatusCode != 200 {
				atomic.AddInt64(&metrics.HTTPError, 1)
			} else {
				atomic.AddInt64(&metrics.HTTP200, 1)
			}
			resp.Body.Close()
			
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func runHistoryQueryTest(wg *sync.WaitGroup) {
	defer wg.Done()
	
	client := &http.Client{Timeout: 10 * time.Second}
	username := fmt.Sprintf("testuser%d", time.Now().Unix()%20+1)
	
	// Login first
	jsonStr := []byte(fmt.Sprintf(`{"username":"%s","password":"password123"}`, username))
	resp, err := client.Post(*serverURL+"/api/v1/auth/login", "application/json", bytes.NewBuffer(jsonStr))
	if err != nil {
		return
	}
	
	var loginResp LoginResponse
	json.NewDecoder(resp.Body).Decode(&loginResp)
	resp.Body.Close()
	
	token := loginResp.Data.Token
	
	req, _ := http.NewRequest("GET", *serverURL+"/api/v1/messages/history?conversation_id=test&limit=50", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	
	for {
		select {
		case <-stopCh:
			return
		default:
			start := time.Now()
			resp, err := client.Do(req)
			latency := time.Since(start).Nanoseconds()
			
			atomic.AddInt64(&metrics.TotalLatency, latency)
			atomic.AddInt64(&metrics.TotalMsgs, 1)
			
			if metrics.MinLatency == 0 || latency < atomic.LoadInt64(&metrics.MinLatency) {
				atomic.StoreInt64(&metrics.MinLatency, latency)
			}
			if latency > atomic.LoadInt64(&metrics.MaxLatency) {
				atomic.StoreInt64(&metrics.MaxLatency, latency)
			}
			
			if err != nil || resp.StatusCode != 200 {
				atomic.AddInt64(&metrics.HTTPError, 1)
			} else {
				atomic.AddInt64(&metrics.HTTP200, 1)
			}
			resp.Body.Close()
			
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func printMetrics() {
	fmt.Println("\n========================================")
	fmt.Println("FINAL METRICS REPORT")
	fmt.Println("========================================")
	
	totalMsgs := atomic.LoadInt64(&metrics.TotalMsgs)
	totalLatency := atomic.LoadInt64(&metrics.TotalLatency)
	minLatency := atomic.LoadInt64(&metrics.MinLatency)
	maxLatency := atomic.LoadInt64(&metrics.MaxLatency)
	
	var avgLatency int64
	if totalMsgs > 0 {
		avgLatency = totalLatency / totalMsgs
	}
	
	fmt.Printf("Total Requests:     %d\n", totalMsgs)
	fmt.Printf("HTTP 200:           %d\n", atomic.LoadInt64(&metrics.HTTP200))
	fmt.Printf("HTTP Errors:        %d\n", atomic.LoadInt64(&metrics.HTTPError))
	fmt.Printf("WS Connected:       %d\n", atomic.LoadInt64(&metrics.WSConnected))
	fmt.Printf("WS Closed:          %d\n", atomic.LoadInt64(&metrics.WSClosed))
	fmt.Printf("WS Messages Sent:   %d\n", atomic.LoadInt64(&metrics.WSMessagesSent))
	fmt.Printf("\n")
	fmt.Printf("Latency (ns):\n")
	fmt.Printf("  Min:    %d\n", minLatency)
	fmt.Printf("  Avg:    %d\n", avgLatency)
	fmt.Printf("  Max:    %d\n", maxLatency)
	fmt.Printf("\n")
	fmt.Printf("Latency (ms):\n")
	fmt.Printf("  Min:    %.2f\n", float64(minLatency)/1e6)
	fmt.Printf("  Avg:    %.2f\n", float64(avgLatency)/1e6)
	fmt.Printf("  Max:    %.2f\n", float64(maxLatency)/1e6)
	fmt.Printf("\n")
	fmt.Printf("Latency (us):\n")
	fmt.Printf("  Min:    %.2f\n", float64(minLatency)/1e3)
	fmt.Printf("  Avg:    %.2f\n", float64(avgLatency)/1e3)
	fmt.Printf("  Max:    %.2f\n", float64(maxLatency)/1e3)
	fmt.Println("========================================")
}
