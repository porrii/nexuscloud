# Interfaz Web (Fase 2 — no implementada todavía)

Este directorio está reservado para la interfaz web de NexusCloud (§44), inspirada conceptualmente en un explorador de archivos moderno (§83), no en una UI minimalista.

Stack decidido (ver [`../docs/architecture.md`](../docs/architecture.md)): **React 18 + TypeScript + Vite**, compilada a estático y embebida en el binario Go vía `embed.FS` — el mismo patrón de "binario autocontenido" que ya usa NexusWorkspace, evita depender de un servidor de assets separado.

Cuando exista, el flag `web.enabled` de `config.yaml` (ya implementado desde la Fase 1, ver `internal/server`) gobernará si esta interfaz se sirve o no — desactivado por defecto (secure by default, §3/§47).
