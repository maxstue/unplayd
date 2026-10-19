# HTTP-Verträge

Versionierte HTTP-Verträge: `catalog-api.openapi.json`, `user-api.openapi.json` und gemeinsame Schemas in `common.json`.

- Validieren und generierten Stand prüfen: `vp run contracts:check`
- Typen für die serverseitigen Nuxt-Clients erzeugen: `vp run contracts:generate`

Fachliche Endpunkte sind als Vertrag definiert und werden in den jeweiligen Umsetzungstickets implementiert.

Service-Grenzen und Dokumentation: [Technische Architektur & Delivery](https://linear.app/justmax/document/unplayd-technische-architektur-and-delivery-90fd6acaa4f3).
