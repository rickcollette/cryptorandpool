# Cryptorandpool

Package cryptorandpool provides a utility for managing a pool of cryptographically secure random data slices.

## Overview: 
This package allows you to efficiently manage random data slices, handle backpressure, and monitor pool usage. It integrates with Redis for distributed data storage and Prometheus for monitoring.

## Usage: 
To use this package, create a CryptoRandPool instance using NewCryptoRandPool, and then use the Get and Put methods to retrieve and return random data slices to the pool. You can also monitor pool usage and perform
automatic resizing using the StartMonitoring method.

**Example:**
```go
import (
        "github.com/rickcollette/cryptorandpool"
        "time"
    )

// Create a CryptoRandPool with specified parameters.
pool, err := cryptorandpool.NewCryptoRandPool(100, 32, "localhost:6379", config, time.Second, 10, 50)
if err != nil {
        // Handle error
    }

// Retrieve a random data slice from the pool.
slice, err := pool.Get(32)
 if err != nil {
    // Handle error
    }

// Use the random data slice for cryptographic operations.

// Put the slice back into the pool when done.
pool.Put(slice)

// Monitor pool usage and perform resizing automatically.
monitoringConfig := cryptorandpool.MonitoringConfig{
        Interval:            time.Minute,
        ResizeIncreaseDelta: 5,
        MaxPoolSize:         200,
        MinPoolSize:         50,
    }
pool.StartMonitoring(monitoringConfig)
``````

## TYPES

```go
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
```
    CryptoRandPool represents a pool of cryptographically secure random data
    slices.

    This type provides methods for retrieving random data slices from the pool
    (Get), returning data slices to the pool (Put), and performing automatic
    pool resizing and monitoring.

```go
type MonitoringConfig struct {
	Interval            time.Duration
	ResizeIncreaseDelta int
	MaxPoolSize         int
	MinPoolSize         int
}
```
    MonitoringConfig defines configuration options for pool monitoring and
    resizing.

    You can use this type to specify the monitoring interval, resize increase
    delta, maximum pool size, and minimum pool size.

```go
type PoolMetrics struct {
	// Has unexported fields.
}
```
    PoolMetrics holds metrics related to pool usage, including counts for empty
    gets, full puts, and backpressure occurrences.

    These metrics are useful for monitoring and optimizing the behavior of the
    CryptoRandPool.

```go
type PrometheusMetricsConfig struct {
	EnableMetrics       bool
	EmptyGetsMetricName string
	FullPutsMetricName  string
}
```

PrometheusMetricsConfig stores configuration options for enabling Prometheus
    metrics and specifying metric names.

    Use NewPrometheusMetricsConfig to create an instance of this type with the
    desired settings.

## FUNCTIONS


```go
func NewCryptoRandPool(size int, sliceSize int, redisAddr string, metricsConfig PrometheusMetricsConfig, backpressureTimeout time.Duration, resizeThreshold, bufferSize int) (*CryptoRandPool, error)
```
NewCryptoRandPool creates a new CryptoRandPool with the specified parameters.
#### Parameters:
- size: The size of the pool.
- sliceSize: The size of each random data slice.
- redisAddr: The address of the Redis instance to use for distributed data storage.
- metricsConfig: An instance of PrometheusMetricsConfig.
- backpressureTimeout: The duration to wait when applying backpressure.
- resizeThreshold: The number of backpressure occurrences to wait before
        resizing the pool.
- bufferSize: The size of the buffer for storing data to be published to
        Redis.

#### Returns:
- *CryptoRandPool: An instance of CryptoRandPool.
- error: An error if one occurred.

```go
func (crp *CryptoRandPool) Get(sliceSize int) ([]byte, error)
```

Get retrieves a random data slice from the pool.
#### Parameters:
- sliceSize: The size of the random data slice to retrieve.

#### Returns:
- []byte: The random data slice.
- error: An error if one occurred.

```go
func (crp *CryptoRandPool) Put(slice []byte)
```
Put returns a random data slice to the pool.

#### Parameters:
- slice: The random data slice to return to the pool.

```go
func (crp *CryptoRandPool) Resize(newSize int, sliceSize int) error
```
Resize resizes the pool to the specified new size.

#### Parameters:
- newSize: The new size for the pool.
- sliceSize: The size of each random data slice.

#### Returns:
- error: An error if one occurred.

```go
func (crp *CryptoRandPool) StartDataSubscription()
```
StartDataSubscription subscribes to the Redis channel for random data.

This method should be called as a goroutine after creating a CryptoRandPool instance.

```go
func (crp *CryptoRandPool) StartMonitoring(config MonitoringConfig) error
```

StartMonitoring starts monitoring pool usage and performing automatic resizing.

#### Parameters:
- config: An instance of MonitoringConfig.

#### Returns:
- error: An error if one occurred.

```go
func NewPrometheusMetricsConfig(enableMetrics bool, emptyGetsName, fullPutsName string) PrometheusMetricsConfig
```
NewPrometheusMetricsConfig creates a new PrometheusMetricsConfig with the provided configuration options.

#### Parameters:
- enableMetrics: Set to true to enable Prometheus metrics collection.
- emptyGetsName: The name for the "empty gets" metric.
- fullPutsName: The name for the "full puts" metric.

#### Returns:
- PrometheusMetricsConfig: An instance of PrometheusMetricsConfig.


```go 
func StartHTTPServerForMetrics(addr string) error
```
StartHTTPServerForMetrics starts an HTTP server for exposing Prometheus metrics.

#### Parameters:
- addr: The address to listen on.

#### Returns:
- error: An error if one occurred.
