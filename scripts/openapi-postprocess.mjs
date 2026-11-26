// Corrige lo que swag no puede expresar en openapi.yaml.
//
// 1. Media types del CSV. swag copia el @Produce de la operación a todas sus
//    respuestas, así que una descarga CSV queda declarando application/json en
//    el 200 y text/csv en los errores. En las operaciones cuyo 200 ofrece
//    text/csv deja solo text/csv en las respuestas 2xx y solo application/json
//    en las de error, que es lo que el servicio responde de verdad.
//
// 2. pattern en propiedades de schemas (swag v1.16 no lee esa etiqueta).
//    title y description de IncidentInput exigen algún carácter que no sea
//    espacio, porque el servicio los recorta y rechaza los que quedan vacíos.
//
// Trabaja por líneas sobre la salida de swagger2openapi (indentación fija de
// dos espacios) para no reformatear el resto del archivo.
// Uso: node scripts/openapi-postprocess.mjs openapi.yaml
import { readFileSync, writeFileSync } from "node:fs";

const file = process.argv[2];
if (!file) {
  console.error("uso: node scripts/openapi-postprocess.mjs <openapi.yaml>");
  process.exit(2);
}

const lines = readFileSync(file, "utf8").split("\n");
const indentOf = (line) => line.length - line.trimStart().length;
const isBlank = (line) => line.trim() === "";

// Devuelve el índice donde termina el bloque que empieza en start (exclusivo).
function blockEnd(start) {
  const indent = indentOf(lines[start]);
  let end = start + 1;
  while (end < lines.length && (isBlank(lines[end]) || indentOf(lines[end]) > indent)) end++;
  return end;
}

// Busca las claves hijas directas de un bloque.
function children(start) {
  const end = blockEnd(start);
  const result = [];
  let childIndent = null;
  for (let i = start + 1; i < end; i++) {
    if (isBlank(lines[i])) continue;
    childIndent ??= indentOf(lines[i]);
    if (indentOf(lines[i]) === childIndent) result.push(i);
  }
  return result;
}

const keyOf = (line) =>
  line
    .trim()
    .replace(/:.*$/, "")
    .replace(/^"(.*)"$/, "$1");
const quotedKeyOf = (line) => {
  const trimmed = line.trim();
  return trimmed.startsWith('"') ? trimmed.slice(1, trimmed.indexOf('"', 1)) : keyOf(line);
};
const childByKey = (start, key) => children(start).find((i) => quotedKeyOf(lines[i]) === key);

const edits = []; // { at, apply }, se aplican de abajo hacia arriba

// 1. Media types del CSV.
let csvOperations = 0;
const pathsLine = lines.findIndex((line) => line === "paths:");
if (pathsLine < 0) {
  console.error(`${file}: no tiene paths`);
  process.exit(1);
}
for (const pathLine of children(pathsLine)) {
  for (const operationLine of children(pathLine)) {
    const responsesLine = childByKey(operationLine, "responses");
    if (responsesLine === undefined) continue;
    const responses = children(responsesLine).map((line) => ({
      code: quotedKeyOf(lines[line]),
      content: childByKey(line, "content"),
    }));
    const ok = responses.find((response) => response.code === "200");
    if (ok?.content === undefined) continue;
    if (!children(ok.content).some((i) => quotedKeyOf(lines[i]) === "text/csv")) continue;

    for (const response of responses) {
      if (response.content === undefined) continue;
      const keep = response.code.startsWith("2") ? "text/csv" : "application/json";
      for (const mediaLine of children(response.content)) {
        if (quotedKeyOf(lines[mediaLine]) === keep) continue;
        const end = blockEnd(mediaLine);
        edits.push({ at: mediaLine, apply: () => lines.splice(mediaLine, end - mediaLine) });
      }
    }
    csvOperations++;
  }
}

// 2. pattern en propiedades: { schema: { propiedad: regex } }.
const patterns = {
  "model.IncidentInput": { title: "\\S", description: "\\S" },
};
let patched = 0;
const componentsLine = lines.findIndex((line) => line === "components:");
const schemasLine = componentsLine < 0 ? undefined : childByKey(componentsLine, "schemas");
for (const [schema, properties] of Object.entries(patterns)) {
  const schemaLine = schemasLine === undefined ? undefined : childByKey(schemasLine, schema);
  const propertiesLine = schemaLine === undefined ? undefined : childByKey(schemaLine, "properties");
  for (const [property, pattern] of Object.entries(properties)) {
    const propertyLine = propertiesLine === undefined ? undefined : childByKey(propertiesLine, property);
    if (propertyLine === undefined) {
      console.error(`${file}: no existe ${schema}.${property}`);
      process.exit(1);
    }
    const keys = children(propertyLine);
    if (keys.some((i) => keyOf(lines[i]) === "pattern")) continue;
    const at = blockEnd(propertyLine);
    const line = `${" ".repeat(indentOf(lines[keys[0]]))}pattern: '${pattern}'`;
    edits.push({ at, apply: () => lines.splice(at, 0, line) });
    patched++;
  }
}

for (const edit of edits.sort((a, b) => b.at - a.at)) edit.apply();
writeFileSync(file, lines.join("\n"));
console.log(`${file}: ${csvOperations} operación(es) CSV corregidas; ${patched} pattern(s) agregados`);
