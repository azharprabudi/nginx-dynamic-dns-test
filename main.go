package main

import (
	"context"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lixiangzhong/dnsutil"
)

type Closer struct {
	wg *sync.WaitGroup
}

const CoreDNSZoneTemplate = `$TTL    604800
@    IN    SOA    ns1.example.com. admin.example.com. (
			 {{.serial}}   ; Serial
             604800        ; Refresh
              86400        ; Retry
            2419200        ; Expire
             604800 )    ; Negative Cache TTL
;

; name servers - NS records
@    IN    NS    ns1

; name servers - A records
ns1.example.com.                                        IN      A       172.16.240.10
{{range $val := .upstreams}}
wsserver.example.com.                                   IN      A       {{$val}}
{{end}}
`

var WebsocketServerUpstreams = [][]string{
	{"172.16.238.10", "172.16.238.11"},
	{"172.16.238.12", "172.16.238.13"},
	{"172.16.238.14", "172.16.238.15"},
}

func rotateWSServerIPAddress(ctx context.Context, closer *Closer, upstreams [][]string, interval time.Duration) {
	serialCount := 1
	rotationIndex := 1
	for {
		select {
		case <-ctx.Done():
			closer.wg.Done()
			return
		default:
			t := template.New("coredns_zone_template")
			t, err := t.Parse(CoreDNSZoneTemplate)
			if err != nil {
				log.Fatalln(err)
			}

			wd, _ := os.Getwd()
			file, err := os.Create(filepath.Join(wd, "containers/coredns/zones/db.example.com"))
			if err != nil {
				log.Fatalln(err)
			}

			log.Printf("CoreDNS: Updating the coredns zone wsserver.example.com [%s, %s]", WebsocketServerUpstreams[rotationIndex][0], WebsocketServerUpstreams[rotationIndex][1])
			err = t.Execute(file, map[string]interface{}{
				"serial":    serialCount,
				"upstreams": WebsocketServerUpstreams[rotationIndex],
			})
			if err != nil {
				log.Fatalln(err)
			}

			if rotationIndex == 2 {
				rotationIndex = -1
			}

			serialCount += 1
			rotationIndex += 1
			time.Sleep(interval)
		}

	}
}

func resolveDomain(ctx context.Context, closer *Closer, domain string, interval time.Duration) {
	for {
		select {
		case <-ctx.Done():
			closer.wg.Done()
			return
		default:
			var dig dnsutil.Dig
			record, err := dig.A(domain)
			if err != nil {
				log.Fatalln(err)
			}

			log.Printf("CoreDNS: DNS Lookup to CoreDNS Server for domain %s [%s,%s]", domain, record[0].A, record[1].A)
			time.Sleep(interval)
		}
	}
}

func wsConnect(ctx context.Context, closer *Closer, host string, interval time.Duration) {
	for {
		select {
		case <-ctx.Done():
			closer.wg.Done()
			return
		default:
			u := url.URL{Scheme: "ws", Host: host, Path: "/"}
			log.Printf("WebSocket %s: Try to connect to %s", host, u.String())

			c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				log.Printf("WebSocket %s: Failed to connect to %s %v", host, u.String(), err)
			} else {
				log.Printf("WebSocket %s: Connection is connected to %s", host, u.String())
				c.Close()
			}

			time.Sleep(interval)
		}
	}
}

func main() {
	wg := sync.WaitGroup{}
	ctx := context.Background()
	duration := 10 * time.Minute
	ctx, cancelFn := context.WithTimeout(ctx, duration)

	// Continuous update the coredns zone every 10s, with the `reload` plugin the coredns no need to restart to update the zone with the new one
	wg.Add(1)
	rotationWSCloser := &Closer{wg: &wg}
	rotationWSInterval := 10 * time.Second
	go rotateWSServerIPAddress(ctx, rotationWSCloser, WebsocketServerUpstreams, rotationWSInterval)

	// Continuous check the domain for websocket server
	wg.Add(1)
	resolveWSCloser := &Closer{wg: &wg}
	domain := "wsserver.example.com"
	resolveInterval := 5 * time.Second
	go resolveDomain(ctx, resolveWSCloser, domain, resolveInterval)

	// Continous to connect & close connection to websocket, to validate if the nginx old proxy runs well
	wg.Add(1)
	nginxOldProxyCloser := &Closer{wg: &wg}
	nginxOldConnectInterval := 5 * time.Second
	go wsConnect(ctx, nginxOldProxyCloser, "nginx-old-proxy", nginxOldConnectInterval)

	// Continous to connect & close connection to websocket, to validate if the nginx new proxy runs well
	wg.Add(1)
	nginxNewProxyCloser := &Closer{wg: &wg}
	nginxNewConnectInterval := 5 * time.Second
	go wsConnect(ctx, nginxNewProxyCloser, "nginx-new-proxy", nginxNewConnectInterval)

	// Continous to connect & close connection to websocket, to validate if the envoy proxy runs well
	wg.Add(1)
	envoyProxyCloser := &Closer{wg: &wg}
	envoyConnectInterval := 5 * time.Second
	go wsConnect(ctx, envoyProxyCloser, "envoy-proxy", envoyConnectInterval)

	signalCh := make(chan os.Signal)
	exit := make(chan bool, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signalCh:
			log.Println("Test: Terminating the application ...")
			cancelFn()
			wg.Wait()
			exit <- true
		case <-ctx.Done():
			log.Println("Test: Terminating the application ...")
			wg.Wait()
			exit <- true
		}
	}()

	log.Println("Test: Run the websocket testing application ...")
	<-exit
	log.Println("Test: Application exited ...")
}
