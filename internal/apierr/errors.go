// Package apierr define el formato de error común de la API
// ({"error": {"code", "message", "details"}}) acordado para todos los servicios.
// Es el único lugar que arma ese cuerpo.
package apierr

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	CodeInvalidBody      = "INVALID_BODY"
	CodeInvalidQuery     = "INVALID_QUERY"
	CodeInvalidParameter = "INVALID_PARAMETER"
	CodeUnauthorized     = "UNAUTHORIZED"
	CodeForbidden        = "FORBIDDEN"
	CodeNotFound         = "NOT_FOUND"
	CodeMethodNotAllowed = "METHOD_NOT_ALLOWED"
	CodeInternal         = "INTERNAL_ERROR"

	CodeIncidentNotFound = "INCIDENT_NOT_FOUND"
)

// StatusClientClosedRequest es el status no estándar con el que se corta una
// request cuya conexión cerró el cliente; nadie lee el body.
const StatusClientClosedRequest = 499

type Body struct {
	Code    string         `json:"code" example:"FORBIDDEN"`
	Message string         `json:"message" example:"No tenés permisos de staff para realizar esta acción."`
	Details map[string]any `json:"details"`
}

type ErrorResponse struct {
	Error Body `json:"error"`
}

// Abort corta la request con el error indicado. details nil se serializa como {}.
func Abort(c *gin.Context, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	c.AbortWithStatusJSON(status, ErrorResponse{Error: Body{Code: code, Message: message, Details: details}})
}

// InvalidBody responde 400 INVALID_BODY. field vacío indica JSON mal formado (details {}).
func InvalidBody(c *gin.Context, field, reason, message string) {
	var details map[string]any
	if field != "" {
		details = map[string]any{"field": field, "reason": reason}
	}
	Abort(c, http.StatusBadRequest, CodeInvalidBody, message, details)
}

// InvalidQuery responde 400 INVALID_QUERY indicando qué parámetro falló.
func InvalidQuery(c *gin.Context, param, message string) {
	Abort(c, http.StatusBadRequest, CodeInvalidQuery, message, map[string]any{"param": param})
}

// InvalidParameter responde 400 INVALID_PARAMETER para un parámetro de path inválido.
func InvalidParameter(c *gin.Context, param, message string) {
	Abort(c, http.StatusBadRequest, CodeInvalidParameter, message, map[string]any{"param": param})
}

func Unauthorized(c *gin.Context) {
	Abort(c, http.StatusUnauthorized, CodeUnauthorized, "Falta una credencial válida.", nil)
}

func Forbidden(c *gin.Context) {
	Abort(c, http.StatusForbidden, CodeForbidden, "No tenés permisos de staff para realizar esta acción.", nil)
}

func NotFound(c *gin.Context) {
	Abort(c, http.StatusNotFound, CodeNotFound, "El recurso solicitado no existe.", nil)
}

func MethodNotAllowed(c *gin.Context) {
	Abort(c, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "El método no está permitido para este recurso.", nil)
}

func Internal(c *gin.Context) {
	Abort(c, http.StatusInternalServerError, CodeInternal, "Error interno del servidor.", nil)
}

// Register configura los errores que genera Gin por su cuenta: ruta inexistente
// (404 NOT_FOUND) y método no permitido (405 METHOD_NOT_ALLOWED).
func Register(router *gin.Engine) {
	router.HandleMethodNotAllowed = true
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	router.NoRoute(NotFound)
	router.NoMethod(MethodNotAllowed)
}
