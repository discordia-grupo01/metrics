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
  --report junit --report-dir "$REPORT_DIR"
