package web

type JSONResultSuccess struct {
	Code    int    `json:"code" example:"200"`
	Message string `json:"message" example:"success"`
}

type JSONResult struct {
	Code    int         `json:"code" example:"200"`
	Message string      `json:"message" example:"success"`
	Data    interface{} `json:"data"`
}

type HTTPError struct {
	Code    int    `json:"code" example:"400"`
	Message string `json:"message" example:"error"`
}
