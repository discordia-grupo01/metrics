# discordia-metrics

Servicio de métricas de actividad y estado de plataforma de Discordia. Mantiene
una proyección local de eventos de RabbitMQ y expone una API HTTP para
administradores de servidores y staff de la plataforma.

## Historias cubiertas

- Ver métricas de actividad de un servidor.
- Informar explícitamente cuando un período no tuvo actividad.
- Restringir las métricas de servidor a usuarios de staff.
- Ver métricas globales para usuarios de staff.
- Consultar la evolución diaria de miembros.
- Exportar el resumen de métricas de un servidor a CSV.
- Publicar, consultar y resolver incidentes de la plataforma.

## Stack

- Lenguaje: Go 1.22.
- Framework HTTP: Gin.
- Base de datos: PostgreSQL 16.
- Eventos: RabbitMQ, exchange `discordia.events`.
- Autenticación externa: Kong valida el JWT; el servicio obtiene el usuario del
  claim `sub`.
- Autorización de servidor: proyección local alimentada por eventos de
  `servers`; no realiza consultas HTTP request-by-request.
- Imagen: Docker multi-stage basada en `golang:1.22-alpine`.

El servicio es dueño de su proyección y de los incidentes. No accede
directamente a las bases de datos de `identify`, `servers` o `messaging`.

## Configuración

Las variables reales se configuran mediante `.env` o el entorno del despliegue.
No se deben commitear archivos `.env`, contraseñas, tokens ni credenciales.
Usar [.env.example](.env.example) como plantilla.

| Variable | Default | Descripción |
|---|---|---|
| `ENVIRONMENT` | `development` | Entorno de ejecución. En `production` Gin usa release mode. |
| `HOST` | `0.0.0.0` | Dirección de escucha HTTP. |
| `PORT` | `8080` | Puerto HTTP. |
| `DATABASE_HOST` | `localhost` | Host de PostgreSQL. |
| `DATABASE_PORT` | `5432` | Puerto de PostgreSQL. |
| `DATABASE_NAME` | `discordia_metrics` | Base de datos de la proyección. |
| `DATABASE_USER` | `discordia` | Usuario de PostgreSQL. |
| `DATABASE_PASSWORD` | `discordia` | Contraseña de PostgreSQL. En producción debe ser obligatoria y secreta. |
| `DATABASE_SSLMODE` | `disable` | `disable` en local; usar `require` o el modo indicado por el proveedor en producción. |
| `DATABASE_URL` | vacío | Cadena PostgreSQL completa. Si está definida, tiene prioridad sobre las variables `DATABASE_*`. |
| `RABBITMQ_URL` | vacío | URL AMQP. Si está vacía, la API arranca pero no consume eventos. |
| `EVENTS_EXCHANGE` | `discordia.events` | Exchange topic compartido por los servicios. |
| `EVENTS_QUEUE` | `metrics` | Cola durable exclusiva de esta proyección. |
| `STAFF_USER_IDS` | vacío | Bootstrap/fallback temporal de UUIDs de staff. La fuente principal son los eventos firmados de `admin`. |
| `ADMIN_EVENTS_HMAC_SECRET` | vacío | Secreto compartido con `admin` para validar cambios de staff recibidos por RabbitMQ. |
| `USER_ID_HEADER` | vacío | Header de identidad para desarrollo local. No configurarlo en producción. |
| `HEALTH_CHECKS` | vacío | Lista separada por comas de `servicio=URL` que metrics consulta para mostrar el estado operativo. Las URLs deben ser las rutas públicas de Kong (`/health/identify`, `/health/servers`, `/health/messaging`, `/health/notifications` y `/health/voice`), no las direcciones privadas de los servicios. |
| `HEALTH_CHECK_INTERVAL` | `30s` | Intervalo entre consultas de health checks. |
| `HEALTH_CHECK_TIMEOUT` | `3s` | Timeout de cada consulta. |
| `HEALTH_FAILURE_THRESHOLD` | `2` | Fallos consecutivos necesarios para marcar un servicio como `degraded`. |
| `HEALTH_RECOVERY_THRESHOLD` | `2` | Respuestas exitosas consecutivas necesarias para recuperar un servicio degradado. |

### Identidad y autorización

En el flujo normal, Kong valida la firma y expiración del JWT y enruta la
solicitud al servicio. Metrics lee el claim `sub` del bearer token. No confía
en un `user_id` enviado como parámetro o en el cuerpo.

Las métricas de un servidor, las métricas globales y la gestión de incidentes
son exclusivas del staff de la plataforma y consultan una proyección local
de los miembros de staff publicada por `admin` mediante eventos RabbitMQ
firmados. `STAFF_USER_IDS` queda como bootstrap/fallback temporal para no
bloquear despliegues durante la migración.

`USER_ID_HEADER` existe únicamente para desarrollo local y pruebas manuales. No
debe configurarse en un entorno expuesto: cualquier cliente que pueda enviar
ese header podría suplantar la identidad.

## API

El contrato completo está en [openapi.yaml](openapi.yaml). Todas las rutas
`/v1/**` requieren autenticación, y Kong aplica JWT antes de llegar al
servicio.

### Métricas de un servidor

- `GET /v1/servers/{serverId}/metrics`
- `GET /v1/servers/{serverId}/metrics/growth`
- `GET /v1/servers/{serverId}/metrics/export.csv`

Los parámetros `from` y `to` son timestamps RFC3339. Si se omiten, se usa el
período de los últimos 30 días. El período máximo permitido es de 366 días y
`from` debe ser anterior a `to`.

El resumen de servidor incluye:

- `active_members`: usuarios distintos que generaron actividad registrada en el
  período. Se consideran actividad los mensajes y los eventos de alta o baja
  de membresía;
- `messages_sent`: mensajes registrados por `messaging.message_created`;
- `new_members`: altas registradas por `servers.member_joined`;
- `has_activity`: indica si alguna de esas métricas es distinta de cero.

Cuando no existe actividad, la respuesta conserva `200 OK`, devuelve contadores
en cero y `has_activity: false`. El cliente puede mostrar un mensaje
contextualizado en lugar de un gráfico vacío.

El endpoint de crecimiento devuelve puntos diarios. Los días sin cambios se
incluyen con el total calculado para ese día.

La exportación devuelve `text/csv` con las columnas:

```text
from,to,active_members,messages_sent,new_members
```

### Métricas globales

`GET /v1/metrics/global` requiere staff y devuelve un resumen agregado de todos
los servidores dentro del período solicitado. Su respuesta es distinta del
resumen de servidor e incluye:

- `active_users`: usuarios distintos que generaron mensajes o cambios de
  membresía;
- `servers_created`: servidores cuyo primer evento `server_updated` ocurrió en
  el período;
- `messages_sent`: mensajes registrados por `messaging.message_created`;
- `has_activity`: indica si alguna de esas métricas es distinta de cero;
- `granularity` y `series`: los mismos tres contadores divididos en tramos
  para graficar. Si el período dura hasta 24 horas los tramos son por hora
  (`hour`); si es más largo, por día (`day`). Cada punto trae `start`, el
  inicio del tramo en UTC. Los tramos arrancan en `from`, así que un cliente
  que manda su medianoche local recibe días alineados con su zona horaria.
  Los usuarios activos de cada tramo no suman el total del período.

### Estado e incidentes

- `GET /v1/status`: devuelve el estado actual (`operational`, `degraded` o
  `maintenance`) y los incidentes activos para cualquier usuario
  autenticado.
- `GET /v1/health/services`: devuelve a staff el último estado observado de
  cada servicio configurado. Es una señal para sugerir un incidente; no crea
  incidentes automáticamente.
- `POST /v1/metrics/incidents`: crea un incidente; requiere staff.
- `POST /v1/metrics/incidents/{id}/resolve`: resuelve un incidente activo;
  requiere staff.

Al publicar o resolver un incidente, metrics publica `platform.status_changed`
en RabbitMQ cuando `RABBITMQ_URL` está configurada. El payload solo contiene el
estado actual y un `event_id`; notifications lo convierte en un broadcast
`status_changed` sobre el tópico `platform` con `refetch: true`. El evento es
un aviso y no reemplaza `GET /v1/status`, que sigue siendo la fuente de verdad.

Cuando un health check cruza el umbral hacia `degraded` o se recupera a
`operational`, metrics publica `platform.health_changed` una sola vez por
transición. Incluye la audiencia de usuarios staff activa; notifications lo
envía por sus tópicos privados como `health_check_changed`, para que el
backoffice muestre la sugerencia y consulte `GET /v1/health/services`. No se
notifica a usuarios comunes ni se crea un incidente automáticamente.

Un incidente contiene título, descripción, servicios afectados y el estado de
plataforma que representa (`degraded` por defecto o `maintenance`). Al
resolverlo deja de aparecer en `GET /v1/status`; si no quedan incidentes
activos, la plataforma vuelve a `operational`.

## Eventos consumidos

Metrics declara la cola `EVENTS_QUEUE` y la vincula al exchange
`EVENTS_EXCHANGE` con estas routing keys:

| Routing key | Proyección |
|---|---|
| `servers.member_joined` | Registra un alta de miembro. |
| `servers.member_left` | Registra una baja de miembro. |
| `servers.server_updated` | Registra la creación/actualización publicada por servers. |
| `servers.server_deleted` | Marca el servidor como eliminado en la proyección de autorización. |
| `servers.role_updated` | Actualiza si un rol contiene `ADMINISTRATOR`. |
| `servers.role_deleted` | Marca un rol como eliminado. |
| `servers.member_roles_updated` | Reemplaza los roles asignados a un usuario. |
| `servers.owner_updated` | Actualiza el propietario del servidor. |
| `messaging.message_created` | Registra un mensaje sin almacenar su texto. |

Cada evento debe tener un `event_id` UUID y `occurred_at`. `event_id` es clave
primaria en `metric_events`, por lo que las entregas duplicadas son
idempotentes. Los eventos inválidos se descartan con `Nack(requeue=false)` para
evitar reintentos infinitos; los errores transitorios de persistencia se
reencolan.

La entrega es eventualmente consistente: una métrica puede tardar en reflejar
un evento hasta que RabbitMQ lo entregue y el consumidor lo persista.

### Códigos de error

- `400`: período o cuerpo inválido;
- `401`: falta autenticación;
- `403`: el usuario autenticado no tiene el permiso requerido, o la
  proyección local todavía no conoce el servidor;
- `404`: el incidente no existe o ya fue resuelto;
- `500`: error interno no clasificado.

## Logs

Cada línea que escribe el servicio es un objeto JSON por la salida estándar, con `time`, `level` (`debug`, `info`, `warning` o `error`), `service` (`metrics`) y `message`. Es el mismo formato que usan `messaging`, `notifications` y `voice-sfu`.

- **Nivel configurable** con `LOG_LEVEL` (`debug`, `info`, `warning` o `error`; por defecto `info`). Un valor inválido hace fallar el arranque con un mensaje claro. Al arrancar escribe `service_started` con el nivel elegido.
- **Qué significa cada nivel:**
  - `error`: falla que impide ejecutar la funcionalidad (no se pudo guardar, la base no responde, un evento agotó los reintentos).
  - `warning`: condición anómala que no impide la funcionalidad básica (un evento descartado por inválido o por firma mala, una conexión a RabbitMQ que se reintenta, una configuración opcional que falta).
  - `info`: acción iniciada por una persona o por el sistema y trazable (evento procesado, barrido de vencimientos, pedido HTTP).
  - `debug`: contexto técnico para diagnosticar (eventos ignorados por viejos o repetidos, `/livez` y `/readyz` cuando responden bien).
- **Pedidos HTTP:** se registran como `http_request` con `method`, `route`, `status` y `duration_ms`. `route` es el **patrón** (`/v1/servers/:serverId`), nunca la ruta real ni la query string, porque ahí viajan ids, invitaciones y tokens. Un pedido sin ruta sale como `unmatched`. Un 5xx se registra como `error`.
- **Qué no se escribe:** el valor de campos llamados `password`, `token`, `ticket`, `jwt`, `authorization`, `cookie`, `secret`, `api_key`, `email` o `content` sale como `[FILTERED]`. Como red de seguridad, cualquier email o JWT que aparezca dentro de un texto (por ejemplo, en el mensaje de un error de la base) se reemplaza por `[EMAIL]` o `[JWT]`.
- **Pánicos:** se registran como `panic_recovered` con la ruta, el error y la traza, sin volcar los headers del pedido.
- El código vive en `internal/logging`. Las llamadas viejas al paquete `log` de Go también salen como JSON, porque `Setup` redirige el logger estándar.

## Trazas y métricas (OpenTelemetry)

El servicio manda trazas y métricas a cualquier backend que hable OTLP por HTTP (New Relic, Grafana Cloud, un colector propio). Está **apagado por defecto**: sin `OTEL_EXPORTER_OTLP_ENDPOINT` no se exporta nada y el servicio funciona igual.

| Variable | Para qué |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | URL base del backend. En New Relic (cuenta de EE. UU.): `https://otlp.nr-data.net` |
| `OTEL_EXPORTER_OTLP_HEADERS` | Credenciales, por ejemplo `api-key=<clave de licencia de ingesta>`. Es un secreto: va solo en las variables de entorno del despliegue, nunca en el repositorio |
| `OTEL_SERVICE_NAME` | Nombre con el que aparece el servicio (por defecto `metrics`) |

- **Pedidos HTTP:** un span por pedido, llamado como el patrón de la ruta (`GET /v1/...`). Solo guarda `http.request.method`, `http.route` y `http.response.status_code`: nunca el path real, la query string ni los headers. Si el pedido trae un `traceparent` (Kong o un cliente), la traza continúa desde ahí.
- **Base de datos:** un span por consulta, con la operación y el SQL con sus `$1`, `$2`, **sin los valores**. Las consultas que no pertenecen a un pedido o a un evento (barridos y reintentos en segundo plano) no generan spans.
- **RabbitMQ:** `metrics` publica directo, sin outbox: el mensaje sale con la cabecera `traceparent` de la traza en curso (por ejemplo, la del pedido que resuelve un incidente).
- **Consumidores:** cada evento que se consume abre un span `<routing key> process` dentro de la traza que llega en la cabecera `traceparent` del mensaje; si falla y se reintenta, el span queda marcado como error. Si el mensaje no la trae, empieza una traza nueva.
- **Qué nunca se exporta:** el valor de los parámetros SQL, el texto de un error (un filtro del exportador le borra `exception.message` y `exception.stacktrace` a todo span) ni datos del usuario.
- **Métricas HTTP:** el histograma `http.server.request.duration` (en segundos, temporalidad delta, exportado cada 60 s) se registra en el mismo middleware que arma el span, con `http.request.method`, `http.route` y `http.response.status_code`; si el estado es 5xx suma `error.type`. De ahí salen el throughput, la latencia y la tasa de errores del resumen de APM en New Relic. Los pedidos sin ruta se agrupan en `unmatched`. Nunca guarda la ruta real ni la query string.
- **Logs:** cada línea escrita dentro de un pedido lleva `trace_id` y `span_id`.

## Envío de logs (New Relic)

Además de la salida estándar, el servicio puede enviar sus logs al Log API de New Relic. Está **apagado por defecto**: solo se activa si hay `LOG_SHIP_URL` y una clave (`LOG_SHIP_API_KEY` o la `api-key` de `OTEL_EXPORTER_OTLP_HEADERS`).

| Variable | Para qué |
|---|---|
| `LOG_SHIP_URL` | URL del Log API. En New Relic (cuenta de EE. UU.): `https://log-api.newrelic.com/log/v1` |
| `LOG_SHIP_API_KEY` | Opcional: clave de ingesta para `LOG_SHIP_URL`. Si no está, se toma la `api-key` de `OTEL_EXPORTER_OTLP_HEADERS`. Es un secreto: va solo en las variables de entorno del despliegue |

- **Qué se envía:** las mismas líneas que salen por la salida estándar, con el mismo filtrado de datos sensibles (campos `password`, `token`, `ticket`, `email`, `content`, etc. y emails o JWT dentro de textos). `trace_id` y `span_id` viajan como `trace.id` y `span.id`, que es como New Relic enlaza cada log con su traza. El servicio se identifica con `service.name` (el valor de `OTEL_SERVICE_NAME`, por defecto `metrics`).
- **Cómo se envía:** `internal/logging` junta las líneas en lotes de hasta 200 o cada 5 segundos, las comprime en gzip y las manda por HTTPS. Los pedidos no esperan al envío.
- **Si el backend falla:** reintenta con espera creciente (de 1 s hasta 60 s) ante errores de red, 408, 429 y 5xx; un lote rechazado de forma definitiva (por ejemplo 400 o 403) se descarta. La cola está acotada a 5.000 entradas: si se llena descarta las más viejas, para no crecer sin límite en memoria.
- **Sin bucles:** los avisos del propio envío (`log_shipping_failed`, `log_shipping_recovered`, `log_batch_discarded`, `log_entries_dropped`) salen solo por la salida estándar y nunca se reenvían.
- **Al apagar:** `logging.Flush()` intenta enviar lo que quedó pendiente durante unos 3 segundos.

## Persistencia y migraciones

Las migraciones ordenadas están en [migrations](migrations). La migración
inicial crea:

- `metric_events`, la proyección de actividad;
- índices por servidor, tipo y fecha;
- `incidents`, el estado de incidentes de plataforma;
- `metric_servers`, `metric_roles`, `metric_member_roles` y `metric_owners`,
  la proyección de autorización local.

En desarrollo standalone, Docker Compose ejecuta el servicio `migrations`
antes de iniciar `api`. Para ejecutar una migración manual:

```bash
set DATABASE_URL=postgres://discordia:discordia@localhost:5432/discordia_metrics?sslmode=disable
make migrate
```

En PowerShell se puede definir la variable con:

```powershell
$env:DATABASE_URL = "postgres://discordia:discordia@localhost:5432/discordia_metrics?sslmode=disable"
make migrate
```

## Despliegue en Render

El servicio puede configurarse en Render como un **Web Service** usando el
`Dockerfile` de este directorio. Activar el deploy automático para la rama
`main`; el workflow de GitHub Actions valida en cada pull request que la
imagen compile y que el contenedor arranque junto con sus dependencias.

Configurar estas variables en Render:

| Variable | Valor recomendado |
|---|---|
| `ENVIRONMENT` | `production` |
| `HOST` | `0.0.0.0` |
| `PORT` | La variable `PORT` proporcionada por Render; si Render no la inyecta, usar `8080`. |
| `DATABASE_HOST` | Host interno de PostgreSQL |
| `DATABASE_PORT` | Puerto interno de PostgreSQL, normalmente `5432` |
| `DATABASE_NAME` | `discordia_metrics` |
| `DATABASE_USER` | Usuario de PostgreSQL |
| `DATABASE_PASSWORD` | Secret de PostgreSQL |
| `DATABASE_SSLMODE` | `require` (o el modo indicado por el proveedor) |
| `RABBITMQ_URL` | URL AMQP del RabbitMQ desplegado; dejar vacía solo si no se consumirán eventos |
| `EVENTS_EXCHANGE` | `discordia.events` |
| `EVENTS_QUEUE` | `metrics` |
| `HEALTH_CHECKS` | Ver configuración recomendada debajo |
| `HEALTH_CHECK_INTERVAL` | `30s` |
| `HEALTH_CHECK_TIMEOUT` | `3s` |
| `HEALTH_FAILURE_THRESHOLD` | `2` |
| `HEALTH_RECOVERY_THRESHOLD` | `2` |
| `STAFF_USER_IDS` | UUIDs de staff separados por comas |

No es necesario configurar `JWT_SECRET`: la validación del JWT pertenece a
Kong y metrics recibe la identidad ya validada. Tampoco configurar
`USER_ID_HEADER` en producción, porque permite suplantar la identidad.

#### Health checks a través de Kong

Metrics consulta los health checks públicos del gateway, no las IPs privadas
por Tailscale. La ruta canónica interna sigue siendo `/readyz` (no
`/healthz`); Kong publica rutas dedicadas que conservan ese path al reenviar.
Configurar en Render reemplazando `<DOMAIN>` por el dominio público de Kong:

```text
HEALTH_CHECKS=identify=https://<DOMAIN>/health/identify,servers=https://<DOMAIN>/health/servers,messaging=https://<DOMAIN>/health/messaging,notifications=https://<DOMAIN>/health/notifications,voice=https://<DOMAIN>/health/voice
```


Configurar `/readyz` como health check de Render. Antes del primer deploy y
cada vez que cambie el esquema, ejecutar las migraciones contra la base de
producción desde un entorno que tenga instalado `migrate`:

```text
migrate -path migrations -database "postgres://<usuario>:<password>@<host>:<puerto>/<base>?sslmode=require" up
```

La imagen actual contiene únicamente el binario de la API; por eso el comando
anterior no está disponible dentro de ella. Render no obtiene automáticamente
las migraciones desde `docker-compose.yml`, que es para desarrollo local y el
smoke test del CI. Usar un job de migración separado o agregar explícitamente
el CLI a una imagen de pre-deploy, manteniendo las variables de conexión como
secrets.

### Usar Supabase como PostgreSQL

Para conectar `metrics` a la base PostgreSQL de Supabase, copiar desde
Supabase Dashboard > Connect la cadena de conexión adecuada para el entorno de
Render y guardarla en Render como `DATABASE_URL`. Usar el pooler recomendado
por Supabase si el proveedor limita las conexiones directas. La cadena debe
incluir usuario, contraseña, host, puerto, base y `sslmode=require`.
El servicio desactiva el cache de prepared statements nombrados para mantener
compatibilidad con ese pooler.

En este servicio no es necesario configurar `SUPABASE_URL`,
`SUPABASE_PUBLISHABLE_KEY`, `SUPABASE_SECRET_KEY` ni `SUPABASE_JWKS_URL`:
`metrics` no llama a la API de Supabase ni valida tokens de Supabase. La
identidad continúa siendo validada por Kong y la base se usa como PostgreSQL
de la proyección de métricas.

Para el workflow de pull requests no se deben agregar secretos de Supabase.
El CI seguirá usando PostgreSQL efímero mediante Docker Compose, de modo que
los PR no dependan de una base de producción.

## Desarrollo local

Requisitos:

- Go 1.22 o compatible;
- Docker Desktop;
- Docker Compose.

```bash
cp .env.example .env
docker compose up --build
```

La API queda disponible en `http://localhost:8080`, PostgreSQL en
`localhost:5432`, RabbitMQ en `localhost:5672` y la consola de RabbitMQ en
`http://localhost:15672`.

Para ejecutar solamente el binario contra dependencias ya levantadas:

```bash
make run
```

Mutation testing de `internal/service` e `internal/health` (`make mutation`,
menos de un minuto): ver [`docs/mutation-testing`](docs/mutation-testing/README.md).

## Estructura

```text
metrics/
├── cmd/api/
│   └── main.go                 configuración, router y ciclo de vida
├── internal/
│   ├── events/                 consumidor RabbitMQ e idempotencia de eventos
│   ├── handler/                adaptadores HTTP y códigos de respuesta
│   ├── middleware/             identidad derivada del JWT/header local
│   ├── model/                  DTOs de métricas e incidentes
│   ├── repository/             consultas y persistencia PostgreSQL
│   └── service/                períodos, autorización y exportación CSV
├── migrations/                 migraciones de PostgreSQL
├── .github/workflows/ci.yml    formato, tests, vet, build e imagen Docker
├── Dockerfile                  imagen multi-stage de producción
├── docker-compose.yml          API, PostgreSQL, migraciones y RabbitMQ
├── Makefile                    comandos de desarrollo
├── openapi.yaml                contrato HTTP
└── .env.example                configuración sin secretos
```

## Integración con el stack

El compose de [infrastructure](../infrastructure/) agrega:

- `metrics-db`;
- `metrics-migrations`;
- `metrics`;
- el volumen `metrics_db_data`;
- la configuración necesaria para RabbitMQ y `servers`.

Kong expone las rutas mediante `metrics-service` y aplica JWT. En un despliegue
distribuido, configurar `METRICS_UPSTREAM_URL` en
[gateway/.env.example](../gateway/.env.example) con la URL interna o de
Tailscale correspondiente.

## Validación

Desde este módulo:

```bash
make openapi
gofmt -l .
go vet ./...
go test ./... -cover
go build ./...
docker build -t discordia-metrics:ci .
```

El workflow de GitHub Actions ejecuta automáticamente `gofmt`, `go build`,
`go vet`, `go test` y el build de la imagen Docker en cada push y pull request.
