"""Hooks de Schemathesis para la segunda corrida del test de contrato.

Con CONTRACT_SEEDED=1 fijan el servidor y el periodo de los datos de seed.sql,
asi las rutas de metricas devuelven contadores reales y no ceros. Sin la
variable no hacen nada (la primera corrida sigue con ids y fechas al azar).
"""

import os

import schemathesis

SEEDED = os.environ.get("CONTRACT_SEEDED") == "1"
SERVER_ID = "00000000-0000-4000-8000-0000000000c1"
FROM = "2026-01-01T00:00:00Z"
TO = "2026-01-15T00:00:00Z"


@schemathesis.hook
def map_path_parameters(context, path_parameters):
    if SEEDED and path_parameters is not None and "serverId" in path_parameters:
        path_parameters["serverId"] = SERVER_ID
    return path_parameters


@schemathesis.hook
def map_query(context, query):
    if SEEDED and query is not None:
        query["from"] = FROM
        query["to"] = TO
    return query
