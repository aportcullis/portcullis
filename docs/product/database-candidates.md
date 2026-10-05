# Container-backed database candidates

**Research record — 2026-10-03.** PostgreSQL has passed the M1 correctness gate. This assessment informs the next adapter decision; candidate priority is our engineering judgment based on the current SQL governance workflow. The [paired PRD](prd.en.md#43-core-access-policies), [dialect decision](../adr/0001-db-driver-and-parser.md), and milestone acceptance remain the product contract.

## Candidate assessment

| Candidate | Docker/Testcontainers for Go | Fit and required work | Proposed priority |
| --- | --- | --- | --- |
| MySQL | [Official module](https://golang.testcontainers.org/modules/mysql/) | Already in the PRD; implement the native driver/parser adapter, TLS, classification, cancellation and implicit-commit DDL handling | First adapter after PostgreSQL |
| MariaDB | [Official module](https://golang.testcontainers.org/modules/mariadb/) | The [chosen MySQL driver's maintainers support MariaDB](https://github.com/go-sql-driver/mysql#requirements). A separate version/SQL acceptance matrix is still required; driver compatibility does not certify parser or governance compatibility | Closest additional candidate after MySQL |
| ClickHouse | [Official module](https://golang.testcontainers.org/modules/clickhouse/) | Fits analysis queries. Its transaction guarantees differ from the baseline. Define read-query-first scope and dedicated classification/type/cancellation contracts | BI expansion candidate |
| SQL Server | [Official module](https://golang.testcontainers.org/modules/mssql/) | Requires a T-SQL adapter and its own transaction/type/cancellation evidence. Container support is Linux x86-64 without emulation; validate on a supported CI host | Demand-led enterprise candidate |
| TiDB | [Official module](https://golang.testcontainers.org/modules/tidb/) | [MySQL compatibility has documented exceptions](https://docs.pingcap.com/tidb/stable/mysql-compatibility/). Qualify SQL forms, result types, locks and DDL independently even if the driver/parser can be reused | After MySQL contracts stabilize |
| CockroachDB | [Official module](https://golang.testcontainers.org/modules/cockroachdb/) | Qualify PostgreSQL-family compatibility separately. [Transaction retry errors](https://docs.cockroachlabs.com/docs/stable/transaction-retry-error-reference) must preserve Portcullis's execution lease and no-automatic-rerun policy | Demand-led distributed SQL candidate |
| MongoDB | [Official module](https://golang.testcontainers.org/modules/mongodb/) | Requires a document-operation request/classification model rather than the current SQL adapter contract | Separate product-scope decision |

Per [ClickHouse transaction documentation](https://clickhouse.com/docs/concepts/features/operations/insert/transactions), insert guarantees and experimental multi-statement transactions differ from the transactional baseline.
Per [Microsoft container support](https://learn.microsoft.com/en-us/sql/linux/install-upgrade/quickstart-install-docker?view=sql-server-ver17), SQL Server containers require Linux x86-64 and emulation is unsupported.


The strongest near-term proposal is MySQL first, then evaluate MariaDB as an explicitly qualified compatibility target. ClickHouse is a plausible subsequent analysis direction; SQL Server should follow demonstrated user demand and an x86-64 test environment. TiDB and CockroachDB need independent compatibility gates. These priorities do not establish support dates.

[ADR-0025](../adr/0025-sql-database-first-expansion.md) establishes SQL-first ordering: PostgreSQL/MySQL parity before WebMCP. SQLite is excluded from supported-target scope. See the [feature support matrix](database-support.md) for current evidence and planned scope.

## Actual engine experiment

The experiment uses the existing Testcontainers for Go dependency (0.43.0) with disposable `GenericContainer` instances, synthetic data, bounded readiness/startup waits, and explicit termination. It executes the image's native SQL CLI over TCP loopback inside the container. No production adapter or additional Go dependency was introduced.
Docker command output is decoded with the SDK's [multiplexed-output option](https://pkg.go.dev/github.com/testcontainers/testcontainers-go/exec#Multiplexed).

Linux ARM64 images resolved for this experiment:

- MySQL: `mysql:8.4.10@sha256:3f616910ccd923d5089d7889d491cefa65e24fcfa79d74db4084729f48c9cab1`.
- MariaDB: `mariadb:11.8.6@sha256:a7a644e095da9f033a8277df91f1cbf8ae9411a9109e2eea2861e7b046d73580`.

These identify the measured engine builds, not the final production/CI version policy. Resolve maintained patch releases and architecture-specific manifests again during adapter implementation.

The smoke checks cover an integer above JavaScript's safe range, an exact decimal and NULL; prepared parameter binding; InnoDB DML rollback; DDL implicit commit; and refusal of an INSERT in a read-only transaction. All CLI connections are container-internal TCP loopback. MySQL passed with its preferred-TLS mode; MariaDB passed with TLS explicitly disabled in the disposable fixture.
Certificate validation and least-privilege credentials were not tested and belong to the adapter acceptance suite.

| Measured engine | Exact integer/decimal/NULL | Prepared bind | InnoDB rollback | DDL implicit commit | Read-only write refusal |
| --- | --- | --- | --- | --- | --- |
| MySQL 8.4.10 | Passed | Passed | Passed: zero rows | Passed: inserted row survives rollback | Passed: error 1792 |
| MariaDB 11.8.6 | Passed | Passed | Passed: zero rows | Passed: inserted row survives rollback | Passed: error 1792 |

Both owned containers were terminated after the checks. Only these two candidates were run; the other entries above are documentation research.

Representative SQL used by the experiment (run on the disposable `probe` database):

```sql
SELECT CAST(9007199254740993 AS UNSIGNED),
       CAST('12345678901234567890.12345678' AS DECIMAL(30,8)), NULL;
CREATE TABLE rollback_probe(id INT PRIMARY KEY) ENGINE=InnoDB;
START TRANSACTION;
INSERT INTO rollback_probe VALUES (1);
ROLLBACK;
SELECT COUNT(*) FROM rollback_probe; -- 0
PREPARE p FROM 'SELECT ?';
SET @v='bound';
EXECUTE p USING @v;
DEALLOCATE PREPARE p;
START TRANSACTION;
INSERT INTO rollback_probe VALUES (3);
CREATE TABLE ddl_probe(id INT) ENGINE=InnoDB;
ROLLBACK;
SELECT COUNT(*) FROM rollback_probe; -- 1: DDL commits the preceding insert
START TRANSACTION READ ONLY;
INSERT INTO rollback_probe VALUES (2); -- refused with error 1792
```

Engine smoke cannot certify the full Portcullis request → distinct approval → single-use execution → encrypted result → audit workflow.
Each promoted database must still pass the shared scenarios, its parser rejection fixtures, exact-value/type handling, timeout/cancellation, TLS/authentication, connection/payload/policy pinning, owner/org isolation, bounded results/CSV and no-retry outcome handling.
