package web

import (
	"github.com/nbigot/minijob/job"
	"github.com/nbigot/minijob/metrics"
)

type JSONResultSuccess struct {
	Code    int    `json:"code" example:"200"`        // The result code
	Message string `json:"message" example:"success"` // The result message
}

type JSONResult struct {
	Code    int         `json:"code" example:"200"`        // The result code
	Message string      `json:"message" example:"success"` // The result message
	Data    interface{} `json:"data"`                      // The result data
}

type JSONResultGetAllJobs struct {
	Code    int        `json:"code" example:"200"`        // The result code
	Message string     `json:"message" example:"success"` // The result message
	Jobs    []*job.Job `json:"jobs"`                      // The jobs
}

type JSONResultGetJobs struct {
	Code       int        `json:"code" example:"200"`        // The result code
	Message    string     `json:"message" example:"success"` // The result message
	Jobs       []*job.Job `json:"jobs"`                      // The jobs
	Total      uint       `json:"total"`                     // The total number of jobs
	Page       uint       `json:"page"`                      // The current page
	Limit      uint       `json:"limit"`                     // The page limit
	TotalPages uint       `json:"totalpages"`                // The total number of pages
}

type JSONResultGetJob struct {
	Code    int      `json:"code" example:"200"`        // The result code
	Message string   `json:"message" example:"success"` // The result message
	Job     *job.Job `json:"job"`                       // The job
}

type JSONResultCreateJob struct {
	Code    int      `json:"code" example:"200"`        // The result code
	Message string   `json:"message" example:"success"` // The result message
	Job     *job.Job `json:"job"`                       // The job
}

type JSONResultCloneJob struct {
	Code    int      `json:"code" example:"200"`        // The result code
	Message string   `json:"message" example:"success"` // The result message
	Job     *job.Job `json:"job"`                       // The job
}

type JSONResultPullJob struct {
	Code    int        `json:"code" example:"200"`        // The result code
	Message string     `json:"message" example:"success"` // The result message
	Jobs    []*job.Job `json:"jobs"`                      // The jobs
}

type JSONResultGetLockedResources struct {
	Code      int                 `json:"code" example:"200"`        // The result code
	Message   string              `json:"message" example:"success"` // The result message
	Resources job.LockedResources `json:"resources"`                 // The locked resources
}

type JSONResultGetJobsMetrics struct {
	Code    int                        `json:"code" example:"200"`        // The result code
	Message string                     `json:"message" example:"success"` // The result message
	Metrics metrics.JobMetricsTopicMap `json:"metrics"`                   // The metrics
}

type JSONResultGetTopics struct {
	Code    int      `json:"code" example:"200"`        // The result code
	Message string   `json:"message" example:"success"` // The result message
	Topics  []string `json:"topics"`                    // The job topics
}

type JSONResultGetTopicsStats struct {
	Code    int                    `json:"code" example:"200"`        // The result code
	Message string                 `json:"message" example:"success"` // The result message
	Topics  []metrics.TopicMetrics `json:"topics"`                    // The job topics with metrics
}

type JSONResultGetResourcesStats struct {
	Code      int                       `json:"code" example:"200"`        // The result code
	Message   string                    `json:"message" example:"success"` // The result message
	Resources []metrics.ResourceMetrics `json:"resources"`                 // The job resources with metrics
}

type TopOldJobsResult struct {
	JobUUID    string `json:"jobuuid"`  // The job UUID
	DurationMs int64  `json:"duration"` // The job duration in milliseconds
}

type TopicOldJobsMap map[string][]TopOldJobsResult

type JSONResultGetOldJobs struct {
	Code    int             `json:"code"`    // The result code
	Message string          `json:"message"` // The result message
	Topics  TopicOldJobsMap `json:"topics"`  // The oldest jobs by topic
}

type HTTPError struct {
	Code    int    `json:"code" example:"400"`      // The result code
	Message string `json:"message" example:"error"` // The error message
}
