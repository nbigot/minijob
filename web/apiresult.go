package web

import "github.com/nbigot/minijob/job"

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

type HTTPError struct {
	Code    int    `json:"code" example:"400"`      // The result code
	Message string `json:"message" example:"error"` // The error message
}
