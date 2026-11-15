#!/usr/bin/env bash
# Valida que metrics cumpla su openapi.yaml con Schemathesis.
# Levanta el servicio contra un Postgres y un RabbitMQ LOCALES y efimeros,
# nunca contra servicios remotos. Uso: make contract-test
# Las dependencias las levanta `make contract-deps-up` (o, en el CI, los
# servicios del job); las migraciones ya tienen que estar aplicadas.
set -euo pipefail

cd "$(dirname "$0")/.."

# Puertos de la tabla del plan de contratos (metrics).
DB_HOST="${CONTRACT_DB_HOST:-localhost}"
DB_PORT="${CONTRACT_DB_PORT:-55502}"
MQ_HOST="${CONTRACT_MQ_HOST:-localhost}"
MQ_PORT="${CONTRACT_MQ_PORT:-55512}"
APP_PORT="${CONTRACT_APP_PORT:-18082}"
MAX_EXAMPLES="${CONTRACT_MAX_EXAMPLES:-25}"
REPORT_DIR="${CONTRACT_REPORT_DIR:-schemathesis-report}"

# Valores de juguete: no son secretos reales.
STAFF_ID="00000000-0000-4000-8000-0000000000a1"
USER_ID="00000000-0000-4000-8000-0000000000b2"

# Guard: se rechaza todo host que no sea local.
for host in "$DB_HOST" "$MQ_HOST"; do
  case "$host" in
    localhost | 127.0.0.1 | ::1) ;;
    *)
      echo "contract-test: host no local '$host'; este script solo corre contra servicios locales." >&2
      exit 1
      ;;
  esac
done

PY="${PYTHON:-python}"
# Schemathesis imprime caracteres que la consola de Windows (cp1252) no puede escribir.
export PYTHONUTF8=1 PYTHONIOENCODING=utf-8

VENV=".contract-venv"
if [ -x "$VENV/bin/schemathesis" ]; then
  ST="$VENV/bin/schemathesis"
elif [ -x "$VENV/Scripts/schemathesis.exe" ]; then
  ST="$VENV/Scripts/schemathesis.exe"
elif command -v schemathesis >/dev/null 2>&1; then
  ST="schemathesis"
else
  "$PY" -m venv "$VENV"
  if [ -x "$VENV/bin/pip" ]; then PIP="$VENV/bin/pip"; else PIP="$VENV/Scripts/pip.exe"; fi
  "$PIP" install -q "schemathesis==4.33.4"
  if [ -x "$VENV/bin/schemathesis" ]; then ST="$VENV/bin/schemathesis"; else ST="$VENV/Scripts/schemathesis.exe"; fi
fi
# Python del mismo entorno que Schemathesis (trae PyYAML) para medir cobertura.
if [ -x "$VENV/bin/python" ]; then PY_VENV="$VENV/bin/python"; elif [ -x "$VENV/Scripts/python.exe" ]; then PY_VENV="$VENV/Scripts/python.exe"; else PY_VENV="$PY"; fi

# metrics no verifica la firma del JWT (la valida Kong): toma el `sub` del payload.
make_token() {
  "$PY" - "$1" <<'PYEOF'
import base64, json, sys

def b64(data):
    return base64.urlsafe_b64encode(json.dumps(data).encode()).rstrip(b"=").decode()

print(b64({"alg": "none"}) + "." + b64({"sub": sys.argv[1]}) + ".contract")
PYEOF
}
TOKEN="$(make_token "$STAFF_ID")"
USER_TOKEN="$(make_token "$USER_ID")"

LOG="$(mktemp)"
export ENVIRONMENT=development HOST=127.0.0.1 PORT="$APP_PORT"
export DATABASE_HOST="$DB_HOST" DATABASE_PORT="$DB_PORT" DATABASE_USER=discordia DATABASE_PASSWORD=discordia
export DATABASE_NAME=discordia_metrics_contract DATABASE_SSLMODE=disable
export RABBITMQ_URL="amqp://guest:guest@$MQ_HOST:$MQ_PORT/"
export STAFF_USER_IDS="$STAFF_ID"
# Sin chequeos de salud salientes: el servicio no hace ninguna llamada de red real.
export HEALTH_CHECKS=""
unset DATABASE_URL USER_ID_HEADER ADMIN_EVENTS_HMAC_SECRET

go build -o .contract-metrics-api ./cmd/api
./.contract-metrics-api >"$LOG" 2>&1 &
APP_PID=$!
cleanup() {
  kill "$APP_PID" 2>/dev/null || true
  # En Windows (Git Bash) kill no alcanza al proceso nativo.
  if command -v taskkill >/dev/null 2>&1; then taskkill //F //IM .contract-metrics-api.exe >/dev/null 2>&1 || taskkill //F //IM .contract-metrics-api >/dev/null 2>&1 || true; fi
  rm -f .contract-metrics-api .contract-metrics-api.exe "$LOG"
}
trap cleanup EXIT

BASE="http://127.0.0.1:$APP_PORT"
ready=0
for _ in $(seq 1 60); do
  if curl -fsS "$BASE/readyz" >/dev/null 2>&1; then
    ready=1
    break
  fi
  if ! kill -0 "$APP_PID" 2>/dev/null; then
    echo "contract-test: el servicio no arrancó:" >&2
    cat "$LOG" >&2
    exit 1
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "contract-test: /readyz no respondió en 60 s:" >&2
  cat "$LOG" >&2
  exit 1
fi

# Schemathesis siempre manda un staff; las ramas 401 y 403 del contrato se
# comprueban a mano con otra identidad.
expect_status() {
  local want="$1" got
  shift
  got="$(curl -s -o /dev/null -w '%{http_code}' "$@")"
  if [ "$got" != "$want" ]; then
    echo "contract-test: se esperaba $want y llegó $got para: $*" >&2
    exit 1
  fi
}
expect_status 401 "$BASE/v1/status"
expect_status 403 -H "Authorization: Bearer $USER_TOKEN" "$BASE/v1/metrics/global"
expect_status 403 -H "Authorization: Bearer $USER_TOKEN" "$BASE/v1/health/services"
expect_status 403 -H "Authorization: Bearer $USER_TOKEN" "$BASE/v1/servers/$USER_ID/metrics"
expect_status 403 -X POST -H "Authorization: Bearer $USER_TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"t","description":"d"}' "$BASE/v1/metrics/incidents"
expect_status 200 -H "Authorization: Bearer $USER_TOKEN" "$BASE/v1/status"

# Checks desactivados (todos los demás quedan activos):
#  - positive_data_acceptance: from/to son RFC3339 validos para el esquema, pero el
#    servicio los rechaza con 400 si from >= to o si el periodo supera 366 dias, y el
#    contrato no puede expresar esa regla entre campos.
#  - negative_data_rejection: el servicio toma `from=` / `to=` vacios como "no enviado"
#    y usa el valor por defecto (contrato: OpenAPI 3.0 lo permite con allowEmptyValue,
#    pero swag no puede generarlo). Rechazarlos cambiaria lo que ve un cliente.
#  - unsupported_method: Gin responde 404 (no 405) a un metodo no declarado; activar
#    HandleMethodNotAllowed cambia el status que ve el cliente.
"$ST" run openapi.yaml \
  --url "$BASE" \
  -H "Authorization: Bearer $TOKEN" \
  --max-examples "$MAX_EXAMPLES" \
  --seed 1 \
  --checks all \
  --exclude-checks positive_data_acceptance,negative_data_rejection,unsupported_method \
  --report junit,har --report-dir "$REPORT_DIR"

# Segunda corrida con datos reales: se siembran eventos y un hook fija el servidor y
# el periodo de esos datos, para validar contra el contrato respuestas con contadores
# distintos de cero (la primera corrida, con ids y fechas al azar, solo ve ceros).
SEED=scripts/contract/seed.sql
if command -v psql >/dev/null 2>&1; then
  PGPASSWORD=discordia psql -h "$DB_HOST" -p "$DB_PORT" -U discordia -d discordia_metrics_contract -q -v ON_ERROR_STOP=1 -f "$SEED"
else
  docker exec -i contract-metrics-pg psql -U discordia -d discordia_metrics_contract -q -v ON_ERROR_STOP=1 <"$SEED"
fi
SEEDED_SERVER="00000000-0000-4000-8000-0000000000c1"
RANGE="from=2026-01-01T00:00:00Z&to=2026-01-15T00:00:00Z"
body="$(curl -fsS -H "Authorization: Bearer $TOKEN" "$BASE/v1/servers/$SEEDED_SERVER/metrics?$RANGE")"
case "$body" in
  *'"messages_sent":4'*) ;;
  *)
    echo "contract-test: los datos sembrados no llegaron a las metricas: $body" >&2
    exit 1
    ;;
esac
CONTRACT_SEEDED=1 SCHEMATHESIS_HOOKS=scripts.contract.hooks PYTHONPATH=. "$ST" run openapi.yaml \
  --url "$BASE" \
  -H "Authorization: Bearer $TOKEN" \
  --max-examples "$MAX_EXAMPLES" \
  --seed 1 \
  --checks all \
  --exclude-checks positive_data_acceptance,negative_data_rejection,unsupported_method \
  --include-path-regex '^/v1/(servers/[^/]+/metrics|metrics/global)' \
  --report junit,har --report-dir "$REPORT_DIR"

# Cobertura de camino feliz: cuantas operaciones del contrato obtuvieron un 2xx real.
"$PY_VENV" scripts/contract/coverage.py openapi.yaml "$REPORT_DIR"/har-*.json
