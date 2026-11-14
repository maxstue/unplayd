# Unplayd

Persönlicher Game Tracker mit Nuxt-BFF, Kotlin/Ktor und Go.

Die Projektdokumentation wird zentral in Linear gepflegt:

- [Unplayd Handbook](https://linear.app/justmax/document/unplayd-handbook-c507394f4486)
- [Getting Started](https://linear.app/justmax/document/unplayd-getting-started-1ffb4410c3d5)
- [Produktbrief](https://linear.app/justmax/document/unplayd-produktbrief-e05a8a5c9405)
- [Technische Architektur & Delivery](https://linear.app/justmax/document/unplayd-technische-architektur-and-delivery-90fd6acaa4f3)
- [Reference](https://linear.app/justmax/document/unplayd-reference-694738ede4dd)
- [Operations](https://linear.app/justmax/document/unplayd-operations-f5e082af98c2)

Alle Anwendungen liegen unter `apps/`: `web`, `user-api` und `catalog-api`.

## Tests

- Web-Unit-Tests (Vitest): `vp run @unplayd/web#test:unit`
- Vue-Komponenten in Chromium (Vitest mit Playwright-Provider): `vp run @unplayd/web#test:browser`
- Browserinteraktionen (Playwright): `vp run @unplayd/web#test:e2e`
- Browser installieren: `pnpm --filter @unplayd/web exec playwright install chromium`

Die E2E-Tests starten die echten Nuxt-Seiten und den BFF über eine separate Test-Layer. Clerk sowie User- und Catalog-HTTP-Services werden durch deterministische Test-Fixtures ersetzt. Die Kotlin- und Go-Integrationssuiten prüfen die echten Serviceimplementierungen und Produktionsmigrationen gegen PostgreSQL-Testcontainer. Test-Fixtures aktivieren keinen Auth-Bypass in der Produktionsapp.

Die User API verwendet einen gemeinsamen JDBC-Pool. Optionale Pool- und Timeout-Einstellungen stehen in [.env.example](./.env.example). Bei der Auslieferung zuerst `vp run db:migrate` ausführen: Katalogmigration 8 reaktiviert früher dauerhaft fehlgeschlagene transiente Refresh-Jobs; neue transiente Fehler werden nach begrenzten Versuchsrunden automatisch erneut eingeplant.

Das BFF begrenzt einzelne Service-Aufrufe standardmäßig auf drei Sekunden und die gesamte Aggregation auf fünf Sekunden (`NUXT_SERVICE_TIMEOUT_MS` / `NUXT_REQUEST_TIMEOUT_MS`). Bereits geladene Bibliothekseinträge bleiben beim Ablauf des Katalogbudgets über Snapshots verfügbar. JSON-Schreibrequests sind vor dem Parsing auf 16 KiB begrenzt, auch bei Chunked Transfer.

Der Katalog verwendet standardmäßig drei Sekunden Verbindungsaufbau, zwei Sekunden Statement- und eine Sekunde Lock-Timeout. Explizite PostgreSQL-URL-Parameter können diese Werte überschreiben. Migrationen verwenden ein getrenntes Budget für DDL (kein Statement-Timeout, zehn Sekunden Lock-Timeout). Ungültige Providerantworten bleiben wiederholbare Fehler. Such-Cursor speichern den Sortiertitel; bei außergewöhnlich langen Titeln bleibt der Cursor auf 512 Zeichen begrenzt und wird bei Änderungen des Grenztreffers mit 400 ungültig. Dann die Suche ab der ersten Seite neu starten.
