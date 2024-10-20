package retrypolicy

// RetryPolicy defines the configuration for the retry policy
type RetryPolicy struct {
	MaxRetry        uint            `yaml:"maxRetry"` // Maximum number of retries
	BackoffStrategy BackoffStrategy `yaml:"backoffStrategy"`
}
