// Package docs GENERATED BY SWAG; DO NOT EDIT
// This file was generated by swaggo/swag
package docs

import "github.com/swaggo/swag"

const docTemplate = `{
    "schemes": {{ marshal .Schemes }},
    "swagger": "2.0",
    "info": {
        "description": "{{escape .Description}}",
        "title": "{{.Title}}",
        "contact": {},
        "license": {
            "name": "MIT",
            "url": "https://github.com/nbigot/minijob/blob/main/LICENSE"
        },
        "version": "{{.Version}}"
    },
    "host": "{{.Host}}",
    "basePath": "{{.BasePath}}",
    "paths": {
        "/": {
            "get": {
                "description": "Home page",
                "produces": [
                    "text/plain"
                ],
                "tags": [
                    "Utils"
                ],
                "summary": "Home page",
                "operationId": "utils-home",
                "responses": {
                    "200": {
                        "description": "Welcome to MiniJob!",
                        "schema": {
                            "type": "string"
                        }
                    }
                }
            }
        },
        "/api/v1/admin/server/restart": {
            "post": {
                "description": "Restart server",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Admin"
                ],
                "summary": "Restart server",
                "operationId": "server-restart",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/admin/server/shutdown": {
            "post": {
                "description": "Shutdown server",
                "consumes": [
                    "application/json"
                ],
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Admin"
                ],
                "summary": "Shutdown server",
                "operationId": "server-shutdown",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/job/": {
            "post": {
                "description": "Create job",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Create job",
                "operationId": "job-create",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultCreateJob"
                        }
                    }
                }
            }
        },
        "/api/v1/job/pull": {
            "post": {
                "description": "Pull one or multiple jobs",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Pull one or multiple jobs",
                "operationId": "job-pull",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultPullJob"
                        }
                    }
                }
            }
        },
        "/api/v1/job/{jobuuid}": {
            "get": {
                "description": "Get a job",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Get a job",
                "operationId": "job-get",
                "parameters": [
                    {
                        "type": "string",
                        "description": "Job UUID",
                        "name": "jobuuid",
                        "in": "path",
                        "required": true
                    }
                ],
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultGetJob"
                        }
                    },
                    "400": {
                        "description": "Invalid UUID",
                        "schema": {
                            "$ref": "#/definitions/github_com_nbigot_minijob_web_apierror.APIError"
                        }
                    },
                    "404": {
                        "description": "Job not found",
                        "schema": {
                            "$ref": "#/definitions/github_com_nbigot_minijob_web_apierror.APIError"
                        }
                    },
                    "500": {
                        "description": "Invalid job",
                        "schema": {
                            "$ref": "#/definitions/github_com_nbigot_minijob_web_apierror.APIError"
                        }
                    }
                }
            },
            "delete": {
                "description": "Delete job",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Delete job",
                "operationId": "job-delete",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/job/{jobuuid}/cancel": {
            "post": {
                "description": "Cancel a job",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Cancel a job",
                "operationId": "job-cancel",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/job/{jobuuid}/clone": {
            "post": {
                "description": "Clone job",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Clone job",
                "operationId": "job-clone",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultCloneJob"
                        }
                    }
                }
            }
        },
        "/api/v1/job/{jobuuid}/fail": {
            "post": {
                "description": "Set job as failed",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Set job as failed",
                "operationId": "job-set-as-failed",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/job/{jobuuid}/succeed": {
            "post": {
                "description": "Set job as successful",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Set job as successful",
                "operationId": "job-set-as-successful",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/job/{jobuuid}/visibilitytimeout": {
            "post": {
                "description": "Change job visibility timeout",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Change job visibility timeout",
                "operationId": "job-change-visibility-timeout",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/jobs/": {
            "get": {
                "description": "Get all jobs",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Get all jobs",
                "operationId": "jobs-get-all",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultGetAllJobs"
                        }
                    }
                }
            },
            "delete": {
                "description": "Delete all jobs",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Delete all jobs",
                "operationId": "jobs-delete-all",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/jobs/monitor": {
            "get": {
                "description": "Monitor jobs",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Jobs"
                ],
                "summary": "Monitor jobs",
                "operationId": "job-monitor",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/api/v1/resources/locked": {
            "get": {
                "description": "Get locked Resources",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Resources"
                ],
                "summary": "Get locked Resources",
                "operationId": "resources-get-locked",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultGetLockedResources"
                        }
                    }
                }
            }
        },
        "/api/v1/resources/unlock": {
            "post": {
                "description": "Unlock all resources",
                "produces": [
                    "application/json"
                ],
                "tags": [
                    "Resources"
                ],
                "summary": "Unlock all resources",
                "operationId": "resources-unlock-all",
                "responses": {
                    "200": {
                        "description": "successful operation",
                        "schema": {
                            "$ref": "#/definitions/web.JSONResultSuccess"
                        }
                    }
                }
            }
        },
        "/computemetrics": {
            "post": {
                "description": "Compute server metrics",
                "produces": [
                    "text/plain"
                ],
                "tags": [
                    "Utils"
                ],
                "summary": "Compute server metrics",
                "operationId": "utils-metrics-compute",
                "responses": {
                    "200": {
                        "description": "ok",
                        "schema": {
                            "type": "string"
                        }
                    }
                }
            }
        },
        "/healthcheck": {
            "get": {
                "description": "Healthcheck server",
                "produces": [
                    "text/plain"
                ],
                "tags": [
                    "Utils"
                ],
                "summary": "Healthcheck server",
                "operationId": "utils-healthcheck",
                "responses": {
                    "200": {
                        "description": "ok",
                        "schema": {
                            "type": "string"
                        }
                    }
                }
            }
        },
        "/metrics": {
            "get": {
                "description": "get server metrics",
                "produces": [
                    "text/plain"
                ],
                "tags": [
                    "Utils"
                ],
                "summary": "get server metrics",
                "operationId": "utils-metrics-get",
                "responses": {
                    "200": {
                        "description": "ok",
                        "schema": {
                            "type": "string"
                        }
                    }
                }
            }
        },
        "/ping": {
            "get": {
                "description": "Ping server",
                "produces": [
                    "text/plain"
                ],
                "tags": [
                    "Utils"
                ],
                "summary": "Ping server",
                "operationId": "utils-ping",
                "responses": {
                    "200": {
                        "description": "ok",
                        "schema": {
                            "type": "string"
                        }
                    }
                }
            }
        }
    },
    "definitions": {
        "github_com_nbigot_minijob_web_apierror.APIError": {
            "type": "object",
            "properties": {
                "code": {
                    "description": "application-specific error code",
                    "type": "integer"
                },
                "details": {
                    "description": "application-level error details that best describes the error, for debugging",
                    "type": "string"
                },
                "error": {
                    "description": "application-level error message, for debugging",
                    "type": "string"
                },
                "jobUUID": {
                    "description": "job uuid",
                    "type": "string"
                },
                "validationErrors": {
                    "description": "list of errors",
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/github_com_nbigot_minijob_web_apierror.ValidationError"
                    }
                }
            }
        },
        "github_com_nbigot_minijob_web_apierror.ValidationError": {
            "type": "object",
            "properties": {
                "failedField": {
                    "type": "string"
                },
                "tag": {
                    "type": "string"
                },
                "value": {
                    "type": "string"
                }
            }
        },
        "job.Job": {
            "type": "object",
            "properties": {
                "debugMode": {
                    "description": "The debug flag of the job (optional)",
                    "type": "boolean"
                },
                "history": {
                    "description": "The list of events of the job",
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/job.JobHistoryEvent"
                    }
                },
                "id": {
                    "description": "The unique identifier of a job (required)",
                    "type": "string"
                },
                "lockResources": {
                    "description": "The list of resources to lock (optional)",
                    "type": "array",
                    "items": {
                        "type": "string"
                    }
                },
                "name": {
                    "description": "The name of the job (optional)",
                    "type": "string"
                },
                "priority": {
                    "description": "The priority of the job (optional)",
                    "type": "integer"
                },
                "properties": {
                    "description": "The job properties (required)",
                    "allOf": [
                        {
                            "$ref": "#/definitions/job.JobProperties"
                        }
                    ]
                },
                "requester": {
                    "description": "The identifier of the job requester (optional)",
                    "type": "string"
                },
                "sessionId": {
                    "description": "The session identifier of the requester (optional)",
                    "type": "string"
                },
                "startAfter": {
                    "description": "Timestamp (in milliseconds) to start the job after",
                    "type": "integer"
                },
                "topic": {
                    "description": "The topic name for which the job has been created (optional)",
                    "type": "string"
                },
                "traceId": {
                    "description": "The trace identifier of the job (optional)",
                    "type": "string"
                },
                "userAgent": {
                    "description": "The user agent (or program name) that make the request (optional)",
                    "type": "string"
                },
                "visibilityTimeout": {
                    "description": "Duration (in seconds) to keep the job hidden from the queue after it is fetched",
                    "type": "integer"
                }
            }
        },
        "job.JobHistoryEvent": {
            "type": "object",
            "properties": {
                "eventType": {
                    "description": "The event type (required)",
                    "type": "string"
                },
                "timestamp": {
                    "description": "The event timestamp (unixmilliseconds) (required)",
                    "type": "integer"
                }
            }
        },
        "job.JobProperties": {
            "type": "object",
            "additionalProperties": true
        },
        "job.LockedResources": {
            "type": "object",
            "additionalProperties": {
                "type": "string"
            }
        },
        "web.JSONResultCloneJob": {
            "type": "object",
            "properties": {
                "code": {
                    "description": "The result code",
                    "type": "integer",
                    "example": 200
                },
                "job": {
                    "description": "The job",
                    "allOf": [
                        {
                            "$ref": "#/definitions/job.Job"
                        }
                    ]
                },
                "message": {
                    "description": "The result message",
                    "type": "string",
                    "example": "success"
                }
            }
        },
        "web.JSONResultCreateJob": {
            "type": "object",
            "properties": {
                "code": {
                    "description": "The result code",
                    "type": "integer",
                    "example": 200
                },
                "job": {
                    "description": "The job",
                    "allOf": [
                        {
                            "$ref": "#/definitions/job.Job"
                        }
                    ]
                },
                "message": {
                    "description": "The result message",
                    "type": "string",
                    "example": "success"
                }
            }
        },
        "web.JSONResultGetAllJobs": {
            "type": "object",
            "properties": {
                "code": {
                    "description": "The result code",
                    "type": "integer",
                    "example": 200
                },
                "jobs": {
                    "description": "The jobs",
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/job.Job"
                    }
                },
                "message": {
                    "description": "The result message",
                    "type": "string",
                    "example": "success"
                }
            }
        },
        "web.JSONResultGetJob": {
            "type": "object",
            "properties": {
                "code": {
                    "description": "The result code",
                    "type": "integer",
                    "example": 200
                },
                "job": {
                    "description": "The job",
                    "allOf": [
                        {
                            "$ref": "#/definitions/job.Job"
                        }
                    ]
                },
                "message": {
                    "description": "The result message",
                    "type": "string",
                    "example": "success"
                }
            }
        },
        "web.JSONResultGetLockedResources": {
            "type": "object",
            "properties": {
                "code": {
                    "description": "The result code",
                    "type": "integer",
                    "example": 200
                },
                "message": {
                    "description": "The result message",
                    "type": "string",
                    "example": "success"
                },
                "resources": {
                    "description": "The locked resources",
                    "allOf": [
                        {
                            "$ref": "#/definitions/job.LockedResources"
                        }
                    ]
                }
            }
        },
        "web.JSONResultPullJob": {
            "type": "object",
            "properties": {
                "code": {
                    "description": "The result code",
                    "type": "integer",
                    "example": 200
                },
                "jobs": {
                    "description": "The jobs",
                    "type": "array",
                    "items": {
                        "$ref": "#/definitions/job.Job"
                    }
                },
                "message": {
                    "description": "The result message",
                    "type": "string",
                    "example": "success"
                }
            }
        },
        "web.JSONResultSuccess": {
            "type": "object",
            "properties": {
                "code": {
                    "description": "The result code",
                    "type": "integer",
                    "example": 200
                },
                "message": {
                    "description": "The result message",
                    "type": "string",
                    "example": "success"
                }
            }
        }
    }
}`

// SwaggerInfo holds exported Swagger Info so clients can modify it
var SwaggerInfo = &swag.Spec{
	Version:          "1.0",
	Host:             "127.0.0.1:8080",
	BasePath:         "/",
	Schemes:          []string{},
	Title:            "MiniJob API",
	Description:      "This documentation describes MiniJob API",
	InfoInstanceName: "swagger",
	SwaggerTemplate:  docTemplate,
}

func init() {
	swag.Register(SwaggerInfo.InstanceName(), SwaggerInfo)
}
