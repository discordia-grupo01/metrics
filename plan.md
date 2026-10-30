# Plan de continuación para `metrics`

## 1. Estado actual del repositorio

El repositorio está en:

`metrics`

Es un módulo Go independiente, integrado con:

- `gateway`
- `infrastructure`
- RabbitMQ y el exchange `discordia.events`
- PostgreSQL
- Servicio `servers` para autorización de permisos

### Funcionalidad implementada

- Métricas por servidor:
  - miembros activos;
  - mensajes enviados;
  - nuevos miembros;
  - crecimiento diario;
  - exportación CSV.
- Métricas globales para staff.
- Consulta de incidentes activos.
- Publicación y resolución de incidentes.
- Health checks `/livez` y `/readyz`.
- Consumidor RabbitMQ idempotente.
- Migración inicial de PostgreSQL.
- Middleware de autenticación basado en JWT o header local.
- Dockerfile y Docker Compose.
- Workflow de CI.
- Rutas de Kong.
- Integración con el Compose principal de infraestructura.
- README y OpenAPI documentados.

### Archivos principales

- `cmd/api/main.go`
- `internal/handler/handler.go`
- `internal/service/service.go`
- `internal/repository/repository.go`
- `internal/events/consumer.go`
- `internal/middleware/auth.go`
- `internal/model/models.go`
- `migrations/000001_init.up.sql`
- `openapi.yaml`
- `README.md`

---

# Plan recomendado

## Fase 1: Revisar y corregir el modelo funcional

Antes de agregar más código, revisar que las métricas reflejen exactamente los criterios de aceptación.

### 1.1 Definir correctamente “miembros activos”

Actualmente la métrica usa usuarios distintos que aparecen en eventos de mensajes o membresía.

Confirmar si “miembros activos” significa:

- usuarios que enviaron mensajes;
- usuarios que tuvieron cualquier actividad;
- miembros que permanecieron activos en el período;
- miembros que estaban presentes al finalizar el período.

La definición debería quedar documentada en:

- código;
- README;
- OpenAPI;
- tests.

### 1.2 Revisar el significado de `new_members`

Actualmente el cálculo global usa eventos de servidores creados como parte de su resumen. Debe verificarse que el campo global corresponda realmente a:

- servidores creados, según la épica global; o
- nuevos miembros, según las métricas de servidor.

Probablemente convenga separar los modelos:

```text
ServerSummary:
- active_members
- messages_sent
- new_members

GlobalSummary:
- active_users
- servers_created
- messages_sent
```

Esto evita reutilizar un DTO con nombres semánticamente ambiguos.

### 1.3 Revisar eventos disponibles

Verificar que los eventos existentes en `events-contract` tengan los campos necesarios:

- `event_id`;
- `occurred_at`;
- `server_id`;
- `user_id`;
- `author_id`.

Si falta algún dato, definir si:

1. se agrega al evento;
2. se deriva;
3. se mantiene una métrica limitada;
4. se crea una proyección complementaria.

Si se modifica un evento, actualizar conjuntamente:

- `events-contract`;
- `docs/asyncapi`;
- productores;
- consumidores;
- tests.

---

## Decisión arquitectónica: autorización local mediante proyección asíncrona

La primera implementación consulta a `servers` por HTTP para autorizar cada
consulta de métricas de un servidor. Esta dependencia síncrona debe
reemplazarse por una proyección local alimentada por RabbitMQ.

El motivo es evitar que una caída, latencia o mantenimiento de `servers`
impida consultar métricas que ya fueron procesadas por `metrics`. Además,
mantiene la separación de responsabilidades recomendada por la cátedra:
`servers` sigue siendo el dueño de servidores, membresías y permisos, mientras
`metrics` mantiene únicamente una réplica de lectura eventual para sus propias
consultas.

### Flujo objetivo

```text
servers
  -> outbox transaccional
  -> RabbitMQ (discordia.events)
  -> metrics
  -> PostgreSQL local de metrics
  -> API de métricas
```

La autorización no debe basarse en consultar la base de datos de `servers`
directamente ni en hacer polling HTTP. Metrics debe consumir eventos que
describan los cambios relevantes de:

- membresías;
- roles y permisos efectivos;
- propietario del servidor;
- suspensión o baja de usuarios, si afecta el acceso.

### Reglas de consistencia

- La respuesta puede ser eventualmente consistente con el estado de
  `servers`.
- Los eventos duplicados deben ser idempotentes.
- Los eventos fuera de orden no deben sobrescribir una versión o estado más
  nuevo.
- La proyección debe conservar `occurred_at` o una versión monotónica por
  agregado.
- Si todavía no existe información local del usuario o servidor, la respuesta
  debe diferenciar “sin datos replicados” de “sin permisos”, sin otorgar
  acceso por defecto.
- Una caída de RabbitMQ o `servers` no debe borrar la proyección ya persistida.
- La recuperación del consumidor debe permitir completar el atraso mediante la
  cola durable y, si fuera necesario, un backfill explícito.

### Trabajo requerido

1. Identificar en `events-contract` los eventos actuales de memberships,
   permisos, roles y ownership.
2. Verificar que sus payloads permitan reconstruir si un usuario tiene
   `ADMINISTRATOR` en un servidor.
3. Si faltan eventos o campos, actualizar en conjunto:
   - `events-contract`;
   - `docs/asyncapi`;
   - productores de `servers`;
   - consumidor de `metrics`;
   - tests de contrato e integración.
4. Crear tablas de proyección locales, sin claves foráneas hacia la base de
   `servers`.
5. ✅ Reemplazar `HTTPAuthorizer` por un `ProjectionAuthorizer` que consulte
   la proyección local.
6. ✅ Definir el comportamiento cuando la proyección todavía está atrasada:
   responder `403` de forma segura, sin permitir por falta de datos.
7. ✅ Eliminar `SERVERS_URL` y la propagación del bearer token; la autorización
   ya no tiene endpoints síncronos.
8. Agregar métricas operativas del consumidor:
   - edad del evento más antiguo pendiente;
   - cantidad de eventos procesados;
   - cantidad de eventos descartados;
   - timestamp de la última sincronización por servidor.

Esta decisión debe considerarse parte del diseño del servicio y quedar
reflejada en el README, OpenAPI si cambia algún código de respuesta, la
configuración de despliegue y los tests.

---

## Fase 2: Completar tests unitarios

Actualmente hay tests básicos para:

- autenticación;
- lectura del claim `sub`;
- rechazo de credenciales ausentes;
- períodos válidos e inválidos.

Agregar tests para:

### 2.1 Servicio

- período por defecto;
- período máximo;
- `from >= to`;
- fechas inválidas;
- exportación CSV;
- formato y headers del CSV;
- autorización exitosa;
- usuario sin `ADMINISTRATOR`;
- error del servicio `servers`.

### 2.2 Handlers

Usar mocks o fakes para probar:

- `200` con actividad;
- `200` sin actividad;
- `400` por período inválido;
- `401` sin autenticación;
- `403` sin permisos;
- `201` al crear incidente;
- `204` al resolver incidente;
- `403` para usuario no staff;
- `500` o `404` según se decida para incidente inexistente.

### 2.3 Eventos

Agregar tests para:

- evento válido;
- evento duplicado;
- evento inválido;
- routing key desconocida;
- `event_id` inválido;
- `occurred_at` vacío;
- error transitorio de persistencia;
- selección correcta de `user_id` y `author_id`.

### 2.4 Repositorio

Agregar tests de integración PostgreSQL para:

- inserción idempotente;
- agregación por servidor;
- agregación global;
- crecimiento diario;
- días sin actividad;
- incidentes activos;
- resolución de incidentes.

La cobertura mínima recomendada debería quedar alineada con los demás servicios, idealmente por encima del 70%.

---

## Fase 3: Corregir el manejo de errores HTTP

Revisar la diferencia entre implementación y contrato.

Actualmente el OpenAPI documenta algunos errores como `502`, mientras que el handler puede devolver `500`.

Definir una política consistente:

```text
400 -> parámetros o cuerpo inválido
401 -> falta autenticación
403 -> autenticado pero sin permisos
404 -> recurso inexistente
502 -> dependencia externa no disponible
503 -> servicio no listo
500 -> error interno no clasificado
```

Luego actualizar:

- handlers;
- tests;
- OpenAPI;
- README.

También conviene reemplazar comparaciones por texto como:

```go
err.Error() == "forbidden"
```

por errores tipados, por ejemplo:

```go
var ErrForbidden = errors.New("forbidden")
```

Esto evita depender de strings.

---

## Fase 4: Mejorar autorización y seguridad

### 4.1 Staff

La variable `STAFF_USER_IDS` es útil como solución inicial, pero no es una fuente de verdad escalable.

Evaluar alternativas:

1. endpoint de autorización del servicio admin;
2. eventos de staff consumidos por metrics;
3. proyección local de miembros staff;
4. claims de staff emitidos por identity/admin.

La opción elegida debería documentarse porque afecta la arquitectura.

### 4.2 JWT

El servicio actualmente interpreta el payload del JWT, pero la validación criptográfica queda en Kong.

Revisar que:

- las rutas de metrics siempre estén protegidas por JWT;
- el servicio no quede expuesto públicamente sin Kong;
- `USER_ID_HEADER` no esté configurado en producción;
- el header de identidad no pueda ser inyectado desde clientes externos.

### 4.3 Migración de autorización síncrona a asíncrona

La autorización de servidor ya no depende de una consulta HTTP request-by-request
a `servers`; usa la proyección local alimentada por RabbitMQ.

Durante la migración:

- mantener temporalmente un feature flag o una estrategia de fallback
  explícita, si se necesita comparar ambas implementaciones;
- no usar el fallback síncrono como polling;
- registrar discrepancias entre la autorización HTTP y la proyección local;
- ✅ retirar `HTTPAuthorizer`, `SERVERS_URL` y el forwarding del token;
- probar el comportamiento con `servers` caído y con RabbitMQ temporalmente
  atrasado.

La implementación final debe consultar solamente la proyección local para
autorizar métricas y debe fallar de manera segura cuando el estado replicado no
esté disponible.

---

## Fase 5: Validar el flujo de RabbitMQ

Probar el flujo completo:

```text
servers / messaging
    -> RabbitMQ
    -> metrics
    -> PostgreSQL
    -> API de métricas
```

### Casos a validar

- RabbitMQ caído durante el arranque.
- RabbitMQ caído después del arranque.
- Mensaje duplicado.
- Mensaje fuera de orden.
- Mensaje inválido.
- Error temporal de PostgreSQL.
- Reinicio del consumidor.
- Cola durable.
- Persistencia de mensajes.
- Reentrega de mensajes.

También verificar si los productores actuales publican con:

- `event_id` único;
- `occurred_at` correcto;
- payload compatible con metrics.

---

## Fase 6: Validar migraciones y base de datos

Ejecutar pruebas sobre una base limpia:

```text
crear base vacía
ejecutar migraciones
iniciar servicio
insertar eventos
consultar métricas
```

También probar upgrade desde una base existente.

Revisar especialmente:

- índices;
- tipos UUID;
- zona horaria;
- consultas de crecimiento diario;
- rendimiento en períodos largos;
- comportamiento con grandes volúmenes de eventos.

Sería conveniente agregar índices adicionales si las pruebas lo justifican:

```text
(server_id, event_type, occurred_at)
(event_type, occurred_at, user_id)
```

No agregarlos sin medir primero.

---

## Fase 7: Validar Docker Compose integrado

Ejecutar desde la raíz de infraestructura:

```powershell
docker compose -f infrastructure\docker-compose.yml config
docker compose -f infrastructure\docker-compose.yml up --build
```

Validar:

- `metrics-db` inicia;
- `metrics-migrations` termina correctamente;
- `metrics` espera migraciones;
- metrics conecta con RabbitMQ;
- metrics conecta con `servers`;
- Kong resuelve `metrics`;
- `/livez` responde;
- `/readyz` responde;
- las rutas JWT llegan al servicio correcto.

Pruebas recomendadas:

```text
GET /v1/status
GET /v1/servers/{id}/metrics
GET /v1/servers/{id}/metrics/growth
GET /v1/servers/{id}/metrics/export.csv
GET /v1/metrics/global
POST /v1/metrics/incidents
POST /v1/metrics/incidents/{id}/resolve
```

---

## Fase 8: Validar Kong

Verificar tanto:

- `kong.yml`
- `kong.yml.template`

Casos a probar:

- request sin JWT;
- JWT vencido;
- JWT válido;
- ruta de servidor;
- ruta global;
- ruta de incidentes;
- ruta no existente;
- método HTTP no permitido.

Confirmar que no haya conflicto entre:

```text
/v1/metrics
/v1/metrics/global
/v1/metrics/incidents
/v1/status
/v1/servers/{serverId}/metrics
```

---

## Fase 9: Revisar OpenAPI como contrato

Antes de considerar cerrado el contrato:

1. Validar sintaxis YAML.
2. Validar OpenAPI 3.0.3 con una herramienta especializada.
3. Comparar cada ruta real de `main.go` con `openapi.yaml`.
4. Comparar códigos HTTP reales con los documentados.
5. Revisar schemas de respuesta reales.
6. Confirmar que `nullable` y formatos sean compatibles.
7. Agregar ejemplos de request y response.

Especialmente revisar:

- error real de `ResolveIncident`;
- respuesta real de `GET /v1/status`;
- respuesta CSV;
- diferencia entre `Summary` de servidor y métrica global;
- comportamiento cuando `points` está vacío.



## Fase 10: CI/CD y despliegue real

El CI actual valida:

```text
gofmt
go test
go vet
go build
docker build
```

Como siguiente paso, agregar progresivamente:

- test de migraciones;
- test de Compose;
- test de contrato OpenAPI;
- escaneo de vulnerabilidades de imagen;
- publicación de imagen en registry;
- deploy automático, si el repositorio sigue el patrón de otros servicios;
- health check post-deploy;
- rollback/documentación operativa.

No agregar deploy productivo hasta definir:

- registry;
- nombre de imagen;
- variables secretas;
- host de PostgreSQL;
- host de RabbitMQ;
- `STAFF_USER_IDS`;
- `METRICS_UPSTREAM_URL`;
- estrategia de migraciones.

---


## Prioridad alta

1. Separar correctamente `GlobalSummary` de `ServerSummary`.
2. Revisar y corregir el cálculo de miembros activos.
3. Agregar tests de handlers y repositorio.
4. Alinear códigos HTTP reales con OpenAPI.
5. Probar RabbitMQ con eventos reales.
6. Ejecutar el stack integrado con Docker Compose.

## Prioridad media

7. Mejorar errores tipados.
8. Agregar tests de integración PostgreSQL.
9. Validar OpenAPI con herramienta específica.
10. Revisar autorización de staff.
11. Agregar ejemplos de API y respuestas.

## Prioridad baja

13. Agregar métricas de rendimiento del propio servicio.
14. Agregar tracing y observabilidad.
15. Automatizar publicación y despliegue de imágenes.
16. Documentar una estrategia formal de retención de eventos.

---

# Contexto compacto para reutilizar en otra conversación

Podés copiar el siguiente texto en una conversación futura:

> Estoy trabajando en `C:\Facultad\IS2\Repos_Discordia\metrics`, un módulo Go 1.22 independiente. El servicio implementa métricas por servidor, métricas globales, crecimiento diario, exportación CSV e incidentes de plataforma. Usa Gin, PostgreSQL, RabbitMQ y Docker. Consume eventos de membresía, servidores, roles, asignaciones de roles, ownership y mensajería desde el exchange `discordia.events`, guardando una proyección idempotente por `event_id`. La autorización de métricas de servidor consulta la proyección local y requiere `ADMINISTRATOR` o ownership; no consulta `servers` de forma síncrona. Métricas globales e incidentes usan `STAFF_USER_IDS`. Kong está configurado en `gateway/kong/kong.yml` y `kong.yml.template`; el stack integrado está en `infrastructure/docker-compose.yml`. El README y `openapi.yaml` ya están documentados. Validaciones realizadas: `go test ./...`, `go vet ./...`, `go build ./...`, gofmt y diff check. Próximas tareas prioritarias: agregar tests de repositorio, probar migraciones y validar el flujo integrado RabbitMQ/PostgreSQL/Docker/Kong. No modificar archivos `.env` ni crear commits automáticamente sin solicitarlo.