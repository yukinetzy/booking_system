package db

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"easybook/internal/config"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func Connect(ctx context.Context, env config.Env) (*mongo.Client, *mongo.Database, error) {
	if strings.HasPrefix(env.MongoURI, "mongodb+srv://") && len(env.DNSServers) > 0 {
		configureDNSServers(env.DNSServers)
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(env.MongoURI))
	if err != nil {
		return nil, nil, fmt.Errorf("connect to mongodb: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, nil, fmt.Errorf("ping mongodb: %w", err)
	}

	return client, client.Database(env.DBName), nil
}

func configureDNSServers(servers []string) {
	normalized := make([]string, 0, len(servers))
	for _, server := range servers {
		server = strings.TrimSpace(server)
		if server == "" {
			continue
		}
		if !strings.Contains(server, ":") {
			server += ":53"
		}
		normalized = append(normalized, server)
	}
	if len(normalized) == 0 {
		return
	}

	var index uint64
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			current := atomic.AddUint64(&index, 1)
			target := normalized[int(current)%len(normalized)]
			dialer := &net.Dialer{Timeout: 5 * time.Second}
			return dialer.DialContext(ctx, "udp", target)
		},
	}
}
