// Package cryptorandpool provides a utility for managing a pool of cryptographically secure random data slices.
//
// Overview:
// This package allows you to efficiently manage random data slices, handle backpressure, and monitor pool usage.
// It integrates with Redis for distributed data storage and Prometheus for monitoring.
//
// Usage:
// To use this package, create a CryptoRandPool instance using NewCryptoRandPool, and then use the Get and Put methods
// to retrieve and return random data slices to the pool. You can also monitor pool usage and perform automatic resizing
// using the StartMonitoring method.
//
// Example:
//   import (
//       "github.com/rickcollette/cryptorandpool"
//       "time"
//   )
//
//   // Create a CryptoRandPool with specified parameters.
//   pool, err := cryptorandpool.NewCryptoRandPool(100, 32, "localhost:6379", config, time.Second, 10, 50)
//   if err != nil {
//       // Handle error
//   }
//
//   // Retrieve a random data slice from the pool.
//   slice, err := pool.Get(32)
//   if err != nil {
//       // Handle error
//   }
//
//   // Use the random data slice for cryptographic operations.
//
//   // Put the slice back into the pool when done.
//   pool.Put(slice)
//
//   // Monitor pool usage and perform resizing automatically.
//   monitoringConfig := cryptorandpool.MonitoringConfig{
//       Interval:            time.Minute,
//       ResizeIncreaseDelta: 5,
//       MaxPoolSize:         200,
//       MinPoolSize:         50,
//   }
//   pool.StartMonitoring(monitoringConfig)
package cryptorandpool

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

// CryptoRandPool represents a pool of cryptographically secure random data slices.
//
// This type provides methods for retrieving random data slices from the pool (Get), returning data slices to the pool (Put),
// and performing automatic pool resizing and monitoring.
type CryptoRandPool struct {
	pool          chan []byte
	metrics       *PoolMetrics
	rwMutex       sync.RWMutex
	metricsConfig PrometheusMetricsConfig
	redisClient   *redis.Client
	backpressureTimeout time.Duration
    resizeThreshold     int
    bufferSize          int
    buffer              chan []byte
	stopCh			  chan struct{}
}

// PoolMetrics holds metrics related to pool usage, including counts for empty gets, full puts, and backpressure occurrences.
//
// These metrics are useful for monitoring and optimizing the behavior of the CryptoRandPool.
type PoolMetrics struct {
    emptyGets        int
    fullPuts         int
    backpressureCount int
}

// MonitoringConfig defines configuration options for pool monitoring and resizing.
//
// You can use this type to specify the monitoring interval, resize increase delta, maximum pool size, and minimum pool size.
type MonitoringConfig struct {
	Interval            time.Duration
	ResizeIncreaseDelta int
	MaxPoolSize         int
	MinPoolSize         int
}

// PrometheusMetricsConfig stores configuration options for enabling Prometheus metrics and specifying metric names.
//
// Use NewPrometheusMetricsConfig to create an instance of this type with the desired settings.
type PrometheusMetricsConfig struct {
	EnableMetrics       bool
	EmptyGetsMetricName string
	FullPutsMetricName  string
}

var (
	emptyGetsCounter prometheus.Counter
	fullPutsCounter  prometheus.Counter
)

// NewPrometheusMetricsConfig creates a new PrometheusMetricsConfig with the provided configuration options.
//
// Parameters:
//   - enableMetrics: Set to true to enable Prometheus metrics collection.
//   - emptyGetsName: The name for the "empty gets" metric.
//   - fullPutsName: The name for the "full puts" metric.
//
// Returns:
//   - PrometheusMetricsConfig: An instance of PrometheusMetricsConfig.
func NewPrometheusMetricsConfig(enableMetrics bool, emptyGetsName, fullPutsName string) PrometheusMetricsConfig {
	return PrometheusMetricsConfig{
		EnableMetrics:       enableMetrics,
		EmptyGetsMetricName: emptyGetsName,
		FullPutsMetricName:  fullPutsName,
	}
}

// registerMetrics registers Prometheus metrics if enabled.
//
// This method should be called after creating a PrometheusMetricsConfig instance to register the necessary metrics.
func (config PrometheusMetricsConfig) registerMetrics() {
	if config.EnableMetrics {
		emptyGetsCounter = prometheus.NewCounter(prometheus.CounterOpts{
			Name: config.EmptyGetsMetricName,
			Help: "Total number of empty gets from the pool",
		})
		fullPutsCounter = prometheus.NewCounter(prometheus.CounterOpts{
			Name: config.FullPutsMetricName,
			Help: "Total number of times the pool was full on put",
		})
		prometheus.MustRegister(emptyGetsCounter, fullPutsCounter)
	}
}

// publishBufferToRedis publishes data from the buffer to Redis at regular intervals.
//
// This method should be called as a goroutine after creating a CryptoRandPool instance.
func (crp *CryptoRandPool) publishBufferToRedis() {
    ticker := time.NewTicker(time.Minute) // Adjust the duration to your needs
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            crp.rwMutex.Lock()
            for len(crp.buffer) > 0 {
                data := <-crp.buffer
                crp.publishToRedis(data)
            }
            crp.rwMutex.Unlock()
        case <-crp.stopCh: // You need to implement a stop channel for graceful shutdown
            return
        }
    }
}

// NewCryptoRandPool creates a new CryptoRandPool with the specified parameters.
//
// Parameters:
//   - size: The size of the pool.
//   - sliceSize: The size of each random data slice.
//   - redisAddr: The address of the Redis instance to use for distributed data storage.
//   - metricsConfig: An instance of PrometheusMetricsConfig.
//   - backpressureTimeout: The duration to wait when applying backpressure.
//   - resizeThreshold: The number of backpressure occurrences to wait before resizing the pool.
//   - bufferSize: The size of the buffer for storing data to be published to Redis.
//
// Returns:
//   - *CryptoRandPool: An instance of CryptoRandPool.
//   - error: An error if one occurred.
func NewCryptoRandPool(size int, sliceSize int, redisAddr string, metricsConfig PrometheusMetricsConfig, backpressureTimeout time.Duration, resizeThreshold, bufferSize int) (*CryptoRandPool, error) {
    pool := make(chan []byte, size)
    buffer := make(chan []byte, bufferSize)
    metrics := &PoolMetrics{}
    metricsConfig.registerMetrics()
    // Initialize Redis client
    client := redis.NewClient(&redis.Options{
        Addr: redisAddr, // e.g., "localhost:6379"
    })

    // Test Redis connection
    ctx := context.Background()
    _, err := client.Ping(ctx).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to connect to Redis: %w", err)
    }

    for i := 0; i < size; i++ {
        slice, err := generateRandomData(sliceSize)
        if err != nil {
            return nil, fmt.Errorf("failed to initialize CryptoRandPool: %w", err)
        }
        pool <- slice
    }

    crp := &CryptoRandPool{
        pool:              pool,
        metrics:           metrics,
        rwMutex:           sync.RWMutex{},
        metricsConfig:     metricsConfig,
        redisClient:       client,
        backpressureTimeout: backpressureTimeout,
        resizeThreshold:   resizeThreshold,
        bufferSize:        bufferSize,
        buffer:            buffer,
    }
	go crp.publishBufferToRedis()
	return crp, nil
}

// Get retrieves a random data slice from the pool.
//
// Parameters:
//   - sliceSize: The size of the random data slice to retrieve.
//
// Returns:
//   - []byte: The random data slice.
//   - error: An error if one occurred.
func (crp *CryptoRandPool) Get(sliceSize int) ([]byte, error) {
    crp.rwMutex.RLock()
    defer crp.rwMutex.RUnlock()

    select {
    case slice := <-crp.pool:
        return slice, nil
    default:
        crp.metrics.emptyGets++
        if crp.metricsConfig.EnableMetrics {
            emptyGetsCounter.Inc()
        }

        // Try to get data from Redis
        return crp.getDataFromRedis(sliceSize)
    }
}

// getDataFromRedis retrieves data from Redis.
//
// This method is called when the local pool is empty.
func (crp *CryptoRandPool) getDataFromRedis(sliceSize int) ([]byte, error) {
    ctx := context.Background()

    listKey := "randDataList"

    val, err := crp.redisClient.BRPop(ctx, 0, listKey).Result() // 0 timeout for blocking
    if err != nil {
        return nil, fmt.Errorf("error retrieving data from Redis: %w", err)
    }

    if len(val) < 2 {
        return nil, fmt.Errorf("invalid data from Redis")
    }

    slice, err := base64.StdEncoding.DecodeString(val[1])
    if err != nil {
        return nil, fmt.Errorf("error decoding data from Redis: %w", err)
    }

    return slice, nil
}

// generateRandomData generates a random data slice of the specified size.
//
// This method is called when initializing the pool.
func generateRandomData(sliceSize int) ([]byte, error) {
    newSlice := make([]byte, sliceSize)
    err := withRetry(3, 500*time.Millisecond, func() error {
        _, err := rand.Read(newSlice)
        return err
    })
    if err != nil {
        return nil, fmt.Errorf("error generating random data: %w", err)
    }
    return newSlice, nil
}

// Put returns a random data slice to the pool.
//
// Parameters:
//   - slice: The random data slice to return to the pool.
func (crp *CryptoRandPool) Put(slice []byte) {
    crp.rwMutex.Lock()
    defer crp.rwMutex.Unlock()

    select {
    case crp.pool <- slice:
        // Successfully put back into the pool
    default:
        crp.applyBackpressure(slice)
    }
}

// applyBackpressure applies backpressure when the pool is full.
//
// This method is called when the local pool is full.
func (crp *CryptoRandPool) applyBackpressure(slice []byte) {
    crp.metrics.fullPuts++
    if crp.metricsConfig.EnableMetrics {
        fullPutsCounter.Inc()
    }

    // Increment a counter each time backpressure is applied
    crp.metrics.backpressureCount++

    // Publish to Redis when backpressure is applied
    crp.publishToRedis(slice)

    // Stage 1: Apply backpressure
    if crp.metrics.backpressureCount <= crp.resizeThreshold {
        log.Printf("Applying backpressure for %v.", crp.backpressureTimeout)
        time.Sleep(crp.backpressureTimeout)
        crp.Put(slice) // Try putting it again
        return
    }

    // Stage 2: Attempt to resize the pool
    if crp.metrics.backpressureCount > crp.resizeThreshold {
        log.Println("Resizing the pool due to sustained backpressure.")
        newSize := len(crp.pool) + crp.resizeThreshold // Example resizing logic
        crp.resizePool(newSize)
        crp.Put(slice) // Try putting it again
        return
    }

    // Stage 3: Buffer to Redis or discard
    select {
    case crp.buffer <- slice:
        log.Println("Buffering data to Redis.")
    default:
        log.Println("Buffer is full. Discarding data.")
    }
}

// resizePool resizes the pool to the specified new size.
//
// This method is called when backpressure is applied and the pool needs to be resized.
func (crp *CryptoRandPool) resizePool(newSize int) error {
    crp.rwMutex.Lock()
    defer crp.rwMutex.Unlock()

    // Check if new size is valid
    if newSize <= 0 {
        return fmt.Errorf("new size for pool must be positive")
    }

    // Create a new pool with the specified new size
newPool := make(chan []byte, newSize)

resizeLoop:
for len(crp.pool) > 0 && len(newPool) < cap(newPool) {
    select {
    case slice := <-crp.pool:
        newPool <- slice
    default:
        break resizeLoop
    }
}

    // Replace the old pool with the new one
    crp.pool = newPool

    return nil
}

// publishToRedis publishes data to Redis.
//
// This method is called when backpressure is applied and the pool is full.
func (crp *CryptoRandPool) publishToRedis(slice []byte) {
    ctx := context.Background()

    // Serialize the data for Redis. Example: convert the byte slice to a base64 string
    serializedData := base64.StdEncoding.EncodeToString(slice)

    // Publish the serialized data to Redis
    err := crp.redisClient.Publish(ctx, "randomDataChannel", serializedData).Err()
    if err != nil {
        log.Printf("Error publishing to Redis: %s\n", err)
    }
}

// Resize resizes the pool to the specified new size.
//
// Parameters:
//   - newSize: The new size for the pool.
//   - sliceSize: The size of each random data slice.
//
// Returns:
//   - error: An error if one occurred.
func (crp *CryptoRandPool) Resize(newSize int, sliceSize int) error {
	crp.rwMutex.Lock()
	defer crp.rwMutex.Unlock()

	newPool := make(chan []byte, newSize)

	close(crp.pool)
	for slice := range crp.pool {
		newPool <- slice
	}

	for i := len(newPool); i < cap(newPool); i++ {
		slice := make([]byte, sliceSize)
		if _, err := rand.Read(slice); err != nil {
			return fmt.Errorf("failed to generate slice during resize: %w", err)
		}
		newPool <- slice
	}

	crp.pool = newPool
	return nil
}

// withRetry retries a function a specified number of times with an exponential backoff.
//
// This method is used when generating random data.
func withRetry(attempts int, sleep time.Duration, f func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = f(); err == nil {
			return nil
		}
		time.Sleep(sleep)
		sleep *= 2
	}
	return err
}

// StartDataSubscription subscribes to the Redis channel for random data.
//
// This method should be called as a goroutine after creating a CryptoRandPool instance.
func (crp *CryptoRandPool) StartDataSubscription() {
    ctx := context.Background()
    pubsub := crp.redisClient.Subscribe(ctx, "randomDataChannel")
    defer pubsub.Close()

    // Wait for confirmation of the subscription
    _, err := pubsub.Receive(ctx)
    if err != nil {
        log.Printf("Failed to subscribe to channel: %v", err)
        return
    }

    ch := pubsub.Channel()
    for msg := range ch {
        // Deserialize the data from the message
        slice, err := base64.StdEncoding.DecodeString(msg.Payload)
        if err != nil {
            log.Printf("Error decoding data from Redis message: %s\n", err)
            continue
        }

        // Try to put the slice into the local pool
        crp.rwMutex.Lock()
        select {
        case crp.pool <- slice:
            // Successfully put into the pool
        default:
            // If the pool is full, handle this situation as needed
            // For example, discard the slice or log the event
            log.Printf("Local pool is full, discarding received data")
        }
        crp.rwMutex.Unlock()
    }
}

// StartMonitoring starts monitoring pool usage and performing automatic resizing.
//
// Parameters:
//   - config: An instance of MonitoringConfig.
//
// Returns:
//   - error: An error if one occurred.
func (crp *CryptoRandPool) StartMonitoring(config MonitoringConfig) error {
	if config.Interval <= 0 {
		return errors.New("monitoring interval must be positive")
	}
	if config.MaxPoolSize < config.MinPoolSize {
		return errors.New("max pool size must be greater than or equal to min pool size")
	}
	if config.ResizeIncreaseDelta <= 0 {
		return errors.New("resize increase delta must be positive")
	}

	go func() {
		for {
			time.Sleep(config.Interval)
			crp.rwMutex.Lock()
			emptyGets := crp.metrics.emptyGets
			fullPuts := crp.metrics.fullPuts
			currentSize := len(crp.pool)
			crp.rwMutex.Unlock()

			if emptyGets > config.ResizeIncreaseDelta && currentSize < config.MaxPoolSize {
				newSize := min(currentSize+config.ResizeIncreaseDelta, config.MaxPoolSize)
				crp.Resize(newSize, currentSize)
			} else if fullPuts > config.ResizeIncreaseDelta && currentSize > config.MinPoolSize {
				newSize := max(currentSize-config.ResizeIncreaseDelta, config.MinPoolSize)
				crp.Resize(newSize, currentSize)
			}

			crp.rwMutex.Lock()
			crp.metrics.emptyGets = 0
			crp.metrics.fullPuts = 0
			crp.rwMutex.Unlock()
		}
	}()
	return nil
}

// min returns the minimum of two integers.
//
// This method is used when resizing the pool.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two integers.
//
// This method is used when resizing the pool.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// StartHTTPServerForMetrics starts an HTTP server for exposing Prometheus metrics.
//
// Parameters:
//   - addr: The address to listen on.
//
// Returns:
//   - error: An error if one occurred.
func StartHTTPServerForMetrics(addr string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(addr, nil)
}
