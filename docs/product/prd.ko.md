# Portcullis — Product Requirements Document

> **언어:** 한국어 · [English](prd.en.md) · [문서 안내](../README.md)
> **동기화 기준:** v0.7 / 2026-10-03. 두 언어의 요구사항과 절 번호는 같은 변경에서 함께 갱신한다.
> **범위 변경(ADR-0025):** 관리 대상은 PostgreSQL/MySQL이며 SQLite는 제외한다. MySQL parity와 SQL 검토·미리보기를 우선하고 MCP Gateway는 M6로 미룬다(ADR-0026/0028).
> **상태:** Draft v0.7 (2026-07-04: §12.2 미결정 항목을 ADR-0001~0012로 해소, 수치·계약 정량화, §4.9 임시 접근 위협 모델 추가)
> **작성일:** 2026-06-27
> **한 줄 정의:** 데이터베이스 접근·변경을 통제·감사하는 DevSecOps 도구이자, 쿼리와 결과를 분석·시각화·공유하는 셀프호스트 오픈소스 BI 도구.
> **문서 역할:** MVP의 범위·정책·인수 조건을 정의하는 제품 계약. 세부 구현 선택은 별도 ADR에서 관리한다.
> **경쟁 기준 갱신:** 2026-09-30, kviklet 0.9.2(2026-09-29 공개) 기준으로 비교·edition·보안 전제를 갱신([ADR-0019](../adr/0019-kviklet-baseline-refresh.md)).

---

## 1. 배경 & 문제 정의

### 1.1 문제
운영 데이터베이스에 대한 사람(개발자/DBA)의 접근은 두 가지를 동시에 요구한다.

- **통제(Governance):** 누가, 언제, 무엇을, 어떤 승인을 거쳐 실행했는가.
  위험한 변경을 사전에 막을 수 있는가.
- **활용(Enablement / BI):** 승인된 쿼리와 결과를 저장·재사용하고, 분석·시각화·공유하여 팀의 의사결정에 활용할 수 있는가.

기존 도구는 한쪽에 치우쳐 있다는 것이 현재 제품 가설이다.

| 도구 | 강점 | Portcullis가 검증할 기회 |
|---|---|---|
| kviklet 0.9.2 | access governance, 개선된 review UI·요청 필터, 결과 저장·dry-run, 임시 웹 session 및 Enterprise DB proxy | version·parameter·공유·즐겨찾기를 가진 쿼리 자산→새 승인 요청 workflow와 schema 거버넌스 통합을 동일 과업으로 검증 |
| Bytebase | access + schema 통합, 기능 방대 | 라이선스가 필요한 self-host HA 대신 무료 단일 인스턴스부터 운영 가능한 좁고 단순한 제품 |
| Atlas (Cloud) | schema 변경 거버넌스, 검증된 diff 엔진 | access governance 없음, 거버넌스 기능이 유료 SaaS에 종속 |
| redash | 쿼리 저장·공유·파라미터화·결과 그리드(쿼리 자산화) | 실행 전 승인·access governance 없음. 자산화 모델을 거버넌스 루프와 결합 |

### 1.2 기회
**"무료 OSS 셀프호스트 + 통제·활용 통합 + 가볍고 깔끔한 UX"** 를 제품 가설로 검증한다.
Portcullis는 governance(kviklet 영역)와 query enablement(redash 영역)를 하나의 통합된 audit 타임라인과 데이터 모델 위에서 결합한다. kviklet도 무료 셀프호스트와 결과 저장을 제공하므로 이 둘만으로 차별성을 주장하지 않는다.
Portcullis 자체 라이선스·유료 경계는 §12.2의 공개 전 결정 사항이다.

### 1.3 핵심 포지셔닝
> kviklet의 access governance + Atlas의 schema 엔진 + redash식 쿼리 자산화를, 외부 SaaS 없이 셀프호스트 OSS로, 하나의 통합 audit과 깔끔한 UX로.

제품의 두 축은 **DevSecOps 데이터베이스 거버넌스**와 **BI 분석·공유**다.
접근 승인·안전한 실행·변경 관리·감사를 쿼리 자산·결과 탐색·차트·대시보드와 연결한다.
MVP의 저장 쿼리와 결과 그리드를 기반으로 BI 기능을 단계적으로 확장한다.

### 1.4 문제 검증 계획
- MVP 구현 전 대상 팀 5곳 이상을 인터뷰하고 최근 운영 DB 접근 사례, 승인 소요시간, 사용 도구와 우회 경로를 기록한다.
- kviklet **0.9.2**/Bytebase로 `요청 탐색→SQL review→승인/반려→실행→결과 cell 확인→과거 쿼리 자산화·재사용` 동일 과업을 수행해 단계 수, 탐색성, 권한 제약을 비교한다. kviklet의 개선된 sidebar·권한별 UI·connection/author/date/type 필터를 기준선에 포함한다.
  UX 우위와 개별 자산화 기능의 부재는 릴리스 노트만으로 단정하지 않는다.
- “좋은 쿼리가 사라진다”는 문제는 최근 30일 쿼리 재실행·Slack/문서 복사 사례로 검증한다.
- 경쟁 제품 기능·라이선스는 변할 수 있으므로 릴리스 계획 시 공식 문서 기준으로 다시 확인한다.
- 인터뷰 결과가 쿼리 자산화 수요를 지지하지 않으면 Core 2 범위를 축소하고 access governance 완성도를 우선한다.

---

## 2. 목표 & 비목표

### 2.1 목표
- 운영 DB 접근(쿼리 실행)을 요청→승인→실행→감사 루프로 통제한다.
- 승인된 쿼리를 저장·북마크·공유·파라미터화하여 팀 자산으로 만든다.
- DevSecOps의 접근·변경 통제와 BI의 분석·시각화·공유를 하나의 제품 흐름으로 제공한다.
  - MVP는 쿼리 자산과 결과 탐색을 제공하고, 후속 단계에서 차트·대시보드를 추가한다.
- 스키마 변경(git 등 remote storage의 migration 파일)을 access core와 동일한 거버넌스 루프(status→dry-run→review→approve→apply→verify)에 태운다.
- MVP는 Docker Compose로 배포하고, 이후 Helm과 Terraform·OpenTofu provider로 확장한다.
- Core 1/2는 단일 Portcullis 바이너리로 제공한다.
  Schema Governance가 추가되는 이미지는 pinned Atlas Community CLI를 함께 포함한다.
- 승인되지 않았거나 승인 후 내용이 바뀐 SQL은 어떤 실행 경로에서도 대상 DB에 도달하지 못하게 한다.

### 2.2 비목표 (명시적 제외)
- **초기 BI 범위:** 스케줄링·BI 알림 엔진, 수십 종 시각화, 임베드/퍼블릭 대시보드는 초기 릴리스 범위 밖이며 필요 시 후속 로드맵에서 검토한다.
- **데이터 이동(ETL/CDC) 도구가 아니다.** 두 connection 간 실시간 스트리밍/자동 데이터 동기화는 하지 않는다.
- **자체 schema diff 엔진을 만들지 않는다.** Atlas(검증된 OSS 엔진)를 빌려 쓴다.
- **자체 엔터프라이즈 IAM/ID 저장소가 되지 않는다.** MVP는 로컬 계정(이메일/비밀번호)과 Google 소셜 로그인(OIDC)을 제공하고, 그 외 OIDC provider·LDAP·SAML·SCIM은 이후 외부 IdP에 위임한다.
- **멀티테넌트 SaaS를 MVP에서 만들지 않는다.** 데이터 모델에 흔적만 남기고, 셀프호스트 single-org로 동작한다.

### 2.3 MVP 릴리스 경계

#### 포함
- **관리 대상 DB:** PostgreSQL, MySQL.
  제품 메타데이터 DB는 관리 대상 종류와 무관하게 PostgreSQL.
- **배포:** 단일 서버 인스턴스 + PostgreSQL로 구성된 Docker Compose quickstart.
- **인증:** 로컬 이메일/비밀번호(argon2id)와 **Google 소셜 로그인(OIDC)**, 서버사이드 세션.
  최초 admin bootstrap 절차 제공.
- **접근 요청:** 하나의 connection을 대상으로 한 단일 SQL statement와 정확한 파라미터 값의 승인 요청.
- **정책:** connection·statement 종류별 필요 승인자 수(`read`/`write`/`ddl` 각 `required_approvals`, 기본 1, 0=자동 승인), 자기 승인 금지, 기본 승인 유효기간 24시간, 승인당 실행 1회.
- **실행:** connection별 허용 statement 종류 정책.
  기본값은 read-only이며 관리자가 write/DDL을 명시적으로 허용.
- **쿼리 자산화:** private 또는 organization-shared 저장 쿼리, 버전, 즐겨찾기, 파라미터, 실행 이력에서 저장.
- **결과:** 최대 10,000행 및 바이트 상한이 있는 임시 결과 스냅샷, 표 탐색과 CSV export.
- **감사:** 인증, 요청, 승인, 실행, 관리 작업의 구조화된 append-only 이벤트.

#### 제외
- PostgreSQL/MySQL 외 추가 관리 대상 DB, 임시 접근 세션과 DB proxy.
- team/role별 승인 규칙, 순서가 있는 다단계 승인, break-glass.
  동일 역할의 N명 정족수 승인은 MVP에 포함.
- Google 외 OIDC provider/LDAP, SAML, SCIM 및 IdP group-role sync.
- 고가용성, Helm, Terraform/OpenTofu provider.
- Schema Change Governance(첫 MVP 다음의 **Schema 마일스톤** 범위).

### 2.4 성공 기준

#### 릴리스 인수 조건
- 승인되지 않은 요청, 만료·반려된 요청, 승인 후 payload가 달라진 요청은 실행 API에서 항상 거부된다.
- `required_approvals=N` 요청은 requester를 제외한 서로 다른 활성 approver N명의 승인이 있어야 실행 가능하고, 한 명의 반려로 terminal `rejected`가 된다.
  `N=0`은 system 자동 승인 event를 남긴다.
- 승인 payload는 `payload_version + organization_id + requester_id + connection_id + connection_config_version + connection_policy_version + statement_class + normalized SQL + typed parameter values`의 canonical JSON을 **`payload_digest`(keyed HMAC-SHA-256)**로 고정하며 실행 직전에 다시 검증한다. digest key는 master key에서 HKDF로 파생한 payload-integrity 전용 key이고, digest에 key version을 함께 저장한다(평문 hash는 audit 장기 보존 시 저엔트로피 literal brute-force에 취약하므로 keyed HMAC 사용).
  SQL 정규화는 UTF-8과 줄바꿈만 통일하고 의미·공백을 재작성하지 않는다.
- connection policy 변경이나 connection archive 이후에는 변경 전에 생성된 미실행 요청을 실행할 수 없다.
- 동일한 승인 완료 요청에 대해 실행 lease를 원자적으로 한 번만 획득한다(`request_id` unique + lease owner/deadline/heartbeat). lease 획득과 `EXECUTION_STARTED` audit는 같은 metadata transaction에 기록한다.
  서버가 결과 확인 전에 중단되면 자동 재시도하지 않고, reconciler가 만료된 `executing`을 `outcome_unknown`으로 전이한다.
- 모든 상태 전이와 실행 시도는 actor, 시각, 대상, 이전/다음 상태, payload digest를 포함한 audit event를 남긴다.
- PostgreSQL/MySQL 모두에서 기본 query timeout 30초, 최대 5분, 최대 10,000행 및 결과 바이트 상한이 서버에서 강제된다.
- Docker Compose 기준 새 환경에서 15분 이내에 bootstrap→connection 등록→요청→승인→실행 흐름을 완료할 수 있다.
- query 실행 시간을 제외한 주요 API는 50 동시 사용자에서 p95 500ms 이내를 목표로 한다.
- **성능 검증·권장 사양(ADR-0020):** k6로 조회·요청·승인을 먼저 측정하고 executor 구현 후 실행·결과 탐색·CSV·취소를 추가한다. active 사용자 수·think time·journey/s·데이터 규모·공용 IP 제한을 함께 명시한다.
  후보 사양(앱+metadata DB 합산 2 vCPU/4 GiB, 4 vCPU/8 GiB, 8 vCPU/16 GiB)은 실험 대상으로만 두며, 반복 부하·soak 통과와 자원 여유를 확인한 뒤 권장 사양 및 팀 규모 환산 근거를 공개한다.
  현재 API-only 측정으로 전체 제품 수용 인원을 보장하지 않는다.
- 수동 승인이 필요한 요청(`required_approvals > 0`)의 `pending→approved|rejected` 결정 시간 중앙값은 30분 이내를 목표로 한다.

#### 베타 제품 가설 검증
- 최소 3개 design partner 팀이 자체 환경에서 설치하고 주간 반복 사용한다.
- 실행 완료된 쿼리의 30% 이상이 저장되거나 기존 saved query에서 시작된다.
- 저장된 shared query의 20% 이상이 30일 내 다른 사용자에게 재사용된다. *(재사용 마찰은 승인 정책에 좌우됨 — 4.3 "재사용 마찰" 참조.)*
- 설치 및 첫 승인 실행까지 걸린 시간의 중앙값이 30분 이하다.

---

## 3. 대상 사용자 & 페르소나

- **개발자(Requester):** 운영 DB에 쿼리를 날려야 한다.
  요청을 올리고 승인을 기다린다.
  자주 쓰는 쿼리를 저장·재사용하고 싶다.
- **승인자(Approver/DBA):** 요청을 검토하고 위험을 판단해 승인/반려한다.
  스키마 변경은 dry-run SQL과 review의 deterministic fact summary(대상 object·table size·알려진 lock 가능성 등)를 보고 게이트한다.
- **관리자(Admin):** connection을 등록하고, 사용자/역할을 관리하고, audit을 본다.

초기 타깃: 자체 호스팅을 선호하는 소규모~중규모 엔지니어링 팀.
요청·승인뿐 아니라 팀 쿼리의 version·공유·재사용과 schema 변경을 같은 audit 흐름에서 관리하려는 조직.
특정 경쟁 제품의 UX가 불편하다는 가정으로 타깃을 정의하지 않는다.

---

## 4. 스코프 — 단계별

### 4.1 Core 1: Access Governance (MVP)
운영 DB 쿼리 실행을 통제하는 핵심 루프.

- **Connection 관리:** PostgreSQL/MySQL 타입별 연결 정보를 등록하고 저장 전 연결 테스트. credential은 애플리케이션 레벨에서 암호화.
- **Access Request:** 사용자가 단일 SQL statement와 실행 시 사용할 파라미터 값을 제출.
  제출 후 payload는 불변.
- **Approval:** 동일 organization의 서로 다른 활성 approver가 정책에 필요한 수만큼 검토→승인하거나 반려.
  자기 요청은 승인할 수 없음.
- **Execute:** 승인된 payload를 1회 실행. timeout·행 수·결과 바이트 상한을 서버에서 강제하고 자동 재시도하지 않음.
- **Audit:** 모든 행위를 구조화된 이벤트로 기록(누가/언제/무엇을/결과).

### 4.2 Core 2: Query Enablement (MVP) — *차별화 핵심*
승인·실행된 쿼리를 자산화한다.

- **쿼리 저장:** 이름·설명·태그·파라미터 정의.
  수정 시 기존 내용을 덮어쓰지 않고 새 version 생성. **한도:** 이름 ≤ 200 code point(비어 있을 수 없음), 설명 ≤ 2,000, 태그 ≤ 10개·각 ≤ 50 code point, 파라미터 ≤ 32개·이름 ≤ 64.
  SQL 본문 상한은 요청 크기 제한(64 KiB, ADR-0010)을 따른다.
- **공유 / 즐겨찾기:** 가시성(private/organization_shared), 권한에 따른 공유, 사용자별 즐겨찾기.
- **실행 이력에서 자산 전환:** audit의 실행 기록에서 "이 쿼리 저장" → saved_query 생성.
- **파라미터화 쿼리:** `:start_date` 같은 명명된 변수를 정의하되 문자열 치환하지 않고 각 DB의 bind parameter로 변환.
  MVP 타입은 string, integer, decimal, boolean, date, timestamp, UUID, null.
  테이블명·컬럼명 등 identifier 파라미터는 금지.
- **승인 비상속:** saved query와 그 version은 승인 상태를 보유하지 않음.
  실행할 때마다 connection과 정확한 파라미터 값을 선택해 새 access request를 생성.
- **결과 그리드:** 같은 결과 스냅샷을 대상으로 정렬·필터·페이지 탐색·CSV export.
  DB 쿼리를 다시 실행하지 않음.

### 4.3 핵심 접근 정책

- **승인 단위:** payload version, normalized SQL, typed parameter values, connection, **connection config version**, requester, statement class, connection policy version을 묶은 immutable payload.
  하나라도 바뀌면 새 요청이 필요. **connection config version이 승인 단위에 들어가는 이유 (2026-07-27 개정, ADR-0014/0018):** connection id는 config를 교체해도 그대로다 — host·port·database·TLS·credential이 전부 그 id 뒤에서 바뀔 수 있다.
  그래서 id만 고정하면 승인 후 대상을 갈아치워도 digest 재검증이 통과하고, 승인자들이 본 것과 다른 데이터베이스에서 문장이 돌 수 있다(OWASP transaction authorization: 거래 데이터가 바뀌면 승인은 무효). **config 교체는 그 connection의 미실행 pending/approved 요청을 같은 트랜잭션에서 `expired(connection_changed)`로 만료시킨다**; draft는 살아남아 새 config로 다시 제출할 수 있다.
  단순 rename은 대상 변경이 아니므로 승인을 죽이지 않는다(그래서 descriptor `version`이 아니라 별도의 `config_version`이다).
- **역할/권한(RBAC, ADR-0008):** 권한은 Google-IAM 스타일 `resource.verb`로 세분화된 **카탈로그(SQL seed)**이고, 역할은 **DB에 저장된 권한 묶음**이다.
  인가는 역할 이름이 아니라 **권한으로 검사**한다.
  시드 시스템 역할 3개는 **default**일 뿐 닫힌 집합이 아니며 admin이 **custom role을 생성**할 수 있다 — `requester`(자기 요청·saved query 관리), `approver`(+ 타인 요청 검토), `admin`(+ 사용자·connection·정책·audit 관리, 전 권한). admin도 자신의 요청은 승인할 수 없음.
- **실행 권한:** 요청자만 자신의 승인된 요청을 실행할 수 있음. admin 대리 실행은 MVP에서 허용하지 않음. kviklet은 0.8.0부터 단일 실행 요청에 타 execute 권한자의 실행을 허용하지만, Portcullis는 requester를 승인 단위와 결과 권한에 포함하는 기존 계약을 유지한다(ADR-0019).
- **소규모 팀 데드락 (결정됨):** 자기 승인 금지(admin 포함) 정책상, 권한자가 1명뿐인 셀프호스트는 자기 요청을 승인할 주체가 없어 실행이 막힌다. connection·종류별 `required_approvals=0` 설정으로 이 데드락을 해소한다.
  이 경우 submit과 동시에 system actor가 자동 승인 event를 남기고 `approved`로 전이한다.
  자기 승인 허용 토글은 governance 약화 우려로 채택하지 않는다.
- **유효기간:** N번째 승인이 기록되거나 system 자동 승인이 발생한 시점부터 기본 24시간. organization 설정으로 15분~7일 범위에서 변경 가능.
  두 경로 모두 **행 잠금을 잡은 트랜잭션 안에서** `expires_at`을 찍는다(2026-07-26 증보): 자동 승인이 잠금 밖에서 시각을 계산하면 정책 갱신·archive와의 잠금 대기 동안 요청이 저장되기도 전에 유효기간이 소모된다.
- **statement 정책:** DB dialect별 SQL parser로 단일 statement와 종류를 판별. parser가 확실히 분류하지 못하면 거부. connection별 `read`, `write`, `ddl` 허용 여부를 적용.
- **read-only 기본값:** 새 connection은 `read=true`, `write=false`, `ddl=false`. write/DDL 활성화는 admin audit event를 남김.
- **재사용 마찰 (결정됨):** saved query는 승인을 상속하지 않으므로(4.2) 매 실행이 새 access request→approval을 거친다.
  이를 **connection·statement 종류별 승인 정책**으로 조절한다(kviklet `numTotalRequired` 모델 검증·확장, 2026-06-27).
  `connection_policy_versions`는 `read`/`write`/`ddl` 각각에 `required_approvals`(0~N, 기본 1)를 둔다.
  저위험 read-only connection을 `read.required_approvals=0`(자동 승인, **audit는 동일하게 기록**)으로 설정하면 재사용 마찰이 해소되고, write/DDL은 더 높은 값으로 게이트한다.
- **정책 snapshot:** submit 전에 statement를 분류하고 현재 connection policy의 version과 적용된 `required_approvals`·limit을 request payload에 고정한다.
  정책 변경은 version을 증가시키고 해당 connection의 아직 실행되지 않은 `pending`/`approved` 요청을 `expired(reason=policy_changed)`로 전이한다.
  기존 승인에 새 정책을 소급 조합하지 않는다.
- **실행 일관성:** metadata DB와 대상 DB 사이에 분산 트랜잭션을 사용하지 않음.
  실행 lease 획득 후 자동 재시도하지 않으며 결과 확인 전 장애는 `outcome_unknown`으로 분류.
- **저장 쿼리 권한:** 작성자와 admin만 수정 가능. shared query는 조직 구성원이 조회·fork할 수 있지만 원본을 덮어쓸 수 없음.
- **결과 권한:** result snapshot과 CSV는 실행 요청자만 조회 가능. approver/admin은 audit metadata를 볼 수 있지만 결과 row를 볼 수 없음.
- **내역 보존 ↔ connection archive 분리 (결정됨):** connection "삭제"는 **archive**이며 요청·승인·실행·audit 내역을 지우지 않는다. archive는 실행 중인 요청이 있으면 거부하고, 성공 시 새 요청·connection test·실행을 즉시 차단하며 `draft`는 `cancelled`, `pending`/`approved`는 `expired(reason=connection_archived)`로 전이한다. connection pool을 닫고 암호화 credential을 폐기하므로 복원 시 credential 재입력과 connection test가 필요하다.
  과거 내역에는 connection ID·표시 이름·DB 종류·target 식별 fingerprint를 snapshot으로 남기되 credential은 남기지 않는다.
  실제 내역 삭제는 별도 retention 작업으로만 수행하며 audit event는 runtime 권한으로 수정·삭제할 수 없다(8.4).

#### MVP DB 호환성 계약

현재 구현 상태는 [DB 기능 지원표](database-support.md)에서 별도로 관리한다. 필수 계약을 현재 제공 기능으로 해석하지 않는다.

| 기능 | PostgreSQL | MySQL |
|---|---|---|
| connection test / TLS 검증 | 필수 | 필수 |
| 단일 statement parse·분류 | 필수 | 필수 |
| typed bind parameter | 필수 | 필수 |
| read/write/DDL policy | 필수 | 필수 |
| timeout·cancel 시도 | 필수 | 필수 |
| row/byte cap·result snapshot·CSV | 필수 | 필수 |
| request→approval→execution→audit | 필수 | 필수 |

- 위 공통 인수 테스트를 통과하지 못한 DB는 UI에서 “지원”으로 표시하지 않는다.
- ADR-0030에 따라 PostgreSQL 16/17/18/19에서 기존 Portcullis 기능의 호환성과 회귀 수정만 유지하며, DB 버전별 신규 기능·문법 확장을 약속하지 않는다. 19를 추가해도 16은 제외하지 않으며, 19는 GA와 최종 검증 전까지 preview로 구분한다. PostgreSQL은 명시적으로 4개 family를 유지하고, MySQL 초기 후보는 최대 3개(8.4 LTS/9.7 LTS/26.7 Innovation), 8.0은 제외한다. 검증 완료·대기, 실제 patch/digest, 실패·skip을 지원 표에 기록한다. Metadata PostgreSQL 18과 성능 검증은 별도 계약이며 범위 확대·축소는 명시적 결정 후 진행한다.
- transaction control, session mutation, DB-native file/network I/O, 여러 statement, parser가 분류하지 못한 statement는 MVP에서 거부한다.
- MySQL DDL처럼 implicit commit이 발생하는 문장은 DDL 허용 정책 아래에서만 실행하고, 승인 화면에 rollback 불가 가능성을 표시한다.

### 4.4 Access Request 상태 머신

```text
draft ──submit(required=0, system approval)──────────────────────────> approved
  │
  └──submit(required>0)──> pending ──approve(count < required)──────> pending
                              ├──────Nth distinct approval──────────> approved
                              └──────reject─────────────────────────> rejected

draft/pending/approved ──requester cancel───────────────────────────> cancelled
draft ──connection archive──────────────────────────────────────────> cancelled
pending/approved ──policy change | connection archive──────────────> expired
approved ──approval invalid─────────────────────────────────────────> expired
approved ──acquire execution lease──> executing ──> succeeded|failed|cancelled|outcome_unknown
```

- `draft`에서만 제목·본문·SQL·파라미터를 수정할 수 있음. submit 시 payload digest를 생성하고 이후 수정 금지. **connection은 생성 시 고정된다 (2026-07-26 개정, ADR-0018):** 대상은 Create가 connection 행을 잠그고 archived를 거부하는 그 시점에 정해지고, 대상이 바뀌면 정책 pin·digest·감사 대상이 모두 달라져 사실상 새 승인 단위다.
  다른 connection을 쓰려면 새 요청을 만든다(기존 draft는 취소).
- **요청 설명 (ADR-0032):** 새 UI 요청은 한 줄 제목(유니코드 코드 포인트 ≤200자)을 필수로 입력하고, 목적과 검토 내용을 적는 일반 텍스트 본문(≤4,000자)은 선택으로 입력한다. 제목은 권한 범위 내 목록·상세에 표시하고, 본문은 SQL·파라미터와 함께 암호화하며 권한이 있는 상세 조회에서만 공개한다. 초안 수정은 같은 version으로 모든 필드를 저장하고 제출 후 설명까지 고정·digest 인증한다. 설명이 없는 기존/API 요청은 유효하며 제목 없음으로 표시한다. 설명도 56 KiB payload 한도를 공유하고 audit metadata에는 넣지 않는다.
- requester는 `draft`, `pending`, `approved` 요청을 취소할 수 있음. terminal 상태는 되돌리지 않음.
- approver는 `pending`만 승인/반려할 수 있고 사유를 남김.
  `(request_id, approver_id)`는 unique이며 requester의 승인은 거부한다.
  N명보다 적게 승인한 동안은 `pending`, N번째 서로 다른 활성 approver가 승인하면 `approved`, 한 명이라도 반려하면 `rejected`.
- `required_approvals=0`은 `approvals` row를 만들거나 가상 사용자를 두지 않고, request 상태 전이와 `actor=system` audit event로 표현한다.
- 승인 기록 시점과 실행 직전에 approver의 활성 상태·권한을 다시 검사한다.
  `pending`에서 기존 승인이 무효가 되면 해당 승인을 count에서 제외하고 계속 `pending`으로 두며, `approved` 이후 유효 승인 수가 기준보다 작아지면 실행하지 않고 `expired(reason=approval_invalidated)`로 전이한다.
- `approved → executing` 전이는 조건부 update로 실행 lease를 원자적으로 획득한 요청 하나만 성공.
  `query_executions.request_id`는 unique이며 lease는 `owner`(server instance id), `deadline`, `heartbeat`를 저장한다. **lease 획득과 `EXECUTION_STARTED` audit event를 같은 metadata transaction에 기록**해, 대상 DB 실행 전에 "시작됐다"는 사실이 항상 남도록 한다(분산 트랜잭션 없이 보장하는 핵심).
- DB 실행이 끝나면 `EXECUTION_FINISHED`와 terminal 상태(succeeded/failed)를 기록한다.
  실행 중 heartbeat로 lease를 갱신한다.
- **장애 복구(reconciler):** startup 및 background reconciler(주기 **30초**)가 heartbeat/deadline이 만료된 `executing`(owner가 죽었거나 응답 없음)을 감지해 `outcome_unknown`으로 전이하고 audit event를 남긴다.
  자동 재실행은 하지 않는다. **lease 수치:** heartbeat **15초** 간격, 매 heartbeat마다 deadline을 `now + 60초`로 연장(= 4 heartbeat 유예) — query timeout(8.2)과 무관하게 heartbeat가 살아 있는 한 lease는 유지되므로 정상 실행을 조기에 뺏지 않고, deadline 경과는 owner 사망을 뜻한다. schema apply lock도 동일 수치·기제를 재사용한다(ADR-0012).
- **late-completion fencing:** 모든 terminal 상태 update는 `state=executing AND owner=? AND attempt_id=?` 조건부 update로만 성공한다. reconciler가 이미 `outcome_unknown`으로 전이한 뒤 원래 worker가 늦게 완료를 보고하면 조건이 불일치해 **상태를 덮어쓰지 못하고**, 대신 `LATE_COMPLETION_OBSERVED` audit event로만 기록한다.
- `executing` 상태에서는 사용자가 cancel을 요청할 수 있으나 DB driver의 취소 성공을 보장하지 않음.
  취소가 확인되면 `cancelled`, 결과를 확인할 수 없으면 `outcome_unknown`으로 기록.
- `succeeded`, `failed`, `outcome_unknown`, `rejected`, `expired`, `cancelled`는 terminal 상태.
- `outcome_unknown`은 자동 재시도하지 않고 운영자가 대상 DB에서 실제 반영 여부를 수동 확인한다.
  확인 결과를 별도 audit event로 남겨 처리 종결 사실을 기록하되, 원래 실행 record의 terminal 상태(`outcome_unknown`)는 사후 변조하지 않는다(append-only 보존).
- **가시성 스코프 (2026-07-23 증보; 2026-07-24 개정, ADR-0018):** **reviewer = `requests.approve` 또는 `requests.reject` 보유자**는 organization 전체 요청을 조회하고 payload를 복호할 수 있으며, 그 외 requester는 **자신의 요청만** 조회한다(서버측 강제). approve/reject는 custom role이 독립적으로 부여할 수 있는 별개 권한이라, 반려만 가능한 사용자도 대상 요청을 볼 수 있어야 하므로 가시성은 두 결정 권한의 합집합이다.
  원문 SQL·파라미터 복호화도 요청자 본인 또는 reviewer에게만 허용하고, 그 외에는 redacted SQL만 제공한다(§8.4).
  목록/집계는 **effective state**(만료 지난 approved=expired) 기준으로 필터·계산해 화면 상태와 일치시킨다.
  요청 **대상 connection 선택**은 `requests.create`로 게이트된 전용 목록(활성 connection·폼 필드만)으로 제공한다 — requester/approver는 `connections.*`를 보유하지 않으므로 관리용 connection 목록에 의존하면 기본 역할이 요청 자체를 만들 수 없다(2026-07-24 증보, ADR-0008/0018).
  SPA 내비게이션·랜딩도 권한 인지로 동작한다.
- **submit 파이프라인·payload 고정 (2026-07-23 증보; 2026-07-24 개정, ADR-0018):** submit은 `BindNamed(:name→$N, 파라미터 검증) → parse → classify → 현재 정책 pin(class 허용 여부·required_approvals) → redact → digest → seal` 순으로 payload를 확정한다.
  `payload_digest`는 암호화·redaction 이전에 **승인 단위 전체(§4.3)의 canonical 직렬화**를 HMAC하고(SQL만이 아니라 org·requester·connection·connection config version·policy version·class·params 결합), 정책 snapshot은 `(connection_id, policy_version)` 복합 FK로 append-only 버전 행을 참조해 고정한다(limit는 join으로 안정).
  실행 시에는 저장된 원문에서 재바인딩하므로 승인·실행 바이트가 동일하다.
  요청 생성(Create)은 항상 `draft`를 남기고 이후 submit만 정책 위반으로 거부되므로, 거부된 submit은 수정 가능한 draft로 남는다.
  결정(승인/반려)은 lock 하에서 approver의 active 상태·action별 권한을 재검사한다(권한 회수 후 terminal 반려 차단). **payload 크기 예산 (2026-07-26 증보):** SQL과 파라미터 이름·값을 **합산해** 요청 크기 제한(64 KiB, ADR-0010) 안에 들어가야 한다 — §4.2의 saved query가 따르는 규칙과 동일하다.
  도메인 한도는 56 KiB이고 나머지는 Connect envelope·id·metadata 몫이다.
  SQL만 재는 방식은 파라미터 값을 무제한으로 남기고(값도 봉인·전송·실행된다), 전송 한도보다 큰 도메인 한도는 API로 도달할 수 없어 문서가 거짓말이 된다. **결정 시각 (2026-07-26 증보):** 수동 승인·반려도 같은 규칙을 따른다 — `approvals.decided_at`을 `clock_timestamp()`로 찍고, 그 하나의 값에서 행의 `updated_at`, 유효기간, 감사 이벤트의 `occurred_at`이 파생된다.
  결정 트랜잭션은 요청 행 잠금에서 대기할 수 있고 `now()`는 BEGIN에 고정되므로, 기본값에 맡기면 대기 이전 시각이 기록되고 유효기간만 앱 시계에서 나와 서로 어긋난다. **자동 승인 시각 (2026-07-26 증보):** `required_approvals=0`의 승인 순간은 connection 행 잠금을 잡은 트랜잭션 안에서 DB가 찍는다.
  행의 `expires_at`, system APPROVED 이벤트의 `occurred_at`, 그 이벤트 metadata의 `expires_at` 복사본이 **모두 그 하나의 시각에서 파생**된다 — 잠금 전에 계산한 값을 감사에 남기면 행과 감사가 서로 다른 시각을 말한다.

### 4.5 Schema Change Governance (Schema 마일스톤)

**단계 분리(ADR-0026):** M3는 불변 artifact 기반 status/dry-run/영향 미리보기만 제공하고, M5에서 승인·apply·복구·verify를 완성한다. M3에는 migration apply를 노출하지 않는다.

Git 등 remote storage의 migration 파일을 access core와 **동일한 거버넌스 패턴**(요청→review→approve→실행→audit)에 태운다.
Atlas Community를 status/dry-run/apply 엔진으로 빌려 쓰고, lint·pre-check 같은 자체 위험 엔진은 만들지 않는다(차별화는 git 연동 + 거버넌스 승인 + impact review + 통합 audit + Argo식 UX에서 나온다).

워크플로우(상태 머신, UI는 Argo Workflow처럼 각 스텝을 시각화):

```
[status] → [dry-run] → [review] → [approve] → [apply] → [verify]
  Atlas      Atlas    자체(+optional AI)  자체게이트  Atlas   Atlas(revision)
```

- **소스:** **Git 등 remote storage 연동.** migration 파일 위치(repo/branch/path)를 connection 단위로 지정. branch는 **최초 선택자일 뿐**이며, versioned migration만 지원하고 declarative workflow는 그 다음.
- **artifact 고정 시점(request 생성 시):** schema request를 만드는 순간 ① branch를 **commit SHA로 resolve** ② 파일을 **immutable artifact로 저장**(remote ID + commit SHA + path + 순서 고정 파일 목록·각 checksum + `atlas.sum` + Atlas version·옵션) ③ 이후 **status→dry-run→review→approve→apply는 모두 이 artifact ID만 사용**한다. branch를 다시 읽지 않으므로 검토한 파일과 승인·적용하는 파일이 항상 같다(중간 branch 변경 TOCTOU 차단).
- **apply 안전장치:** apply는 connection별 **migration lock**을 잡고 status를 재검증한 뒤 artifact의 파일 수만큼만 적용한다(Atlas `migrate apply`는 directory와 revision history 일치 전제). lock은 서버 장애 후 영구 점유되지 않도록 **owner/deadline/heartbeat 또는 PostgreSQL advisory lock 기반 복구 규칙**을 둔다. apply는 일반 쿼리보다 길 수 있어 별도 **`schema_apply_timeout`**을 적용한다.
- **status:** connection별로 **얼마나 적용됐는지**(applied vs pending) 표시.
  Atlas `migrate status`.
- **dry-run:** pending SQL preview.
  Atlas `--dry-run`은 실행 효과 시뮬레이션이 아니라 적용될 SQL 미리보기로 표현. review의 선행 입력이다.
- **review:** dry-run SQL을 입력으로 승인자에게 **deterministic fact summary**를 제공한다 — statement 종류, 대상 object, 현재 table size·추정 row 수, DB·버전별로 **알려진 lock/table-rewrite 가능성**. **결정론 규칙:** 각 fact의 산출원은 dialect adapter의 native catalog 질의로 항목별 고정한다(예: PG table size=`pg_table_size`, 추정 row=`pg_class.reltuples`; lock/rewrite 가능성은 엔진·버전별 정적 lookup 표).
  산출원이 정의되지 않은 엔진·항목, 계산할 수 없는 항목(특히 DDL의 실제 영향 row, rewrite 여부)은 **명시적으로 `unknown`**으로 표기한다 — 같은 입력·같은 DB 상태면 항상 같은 summary가 나오고, 추정값을 사실처럼 약속하지 않는다.
  AI Review(4.8)는 이 fact를 **설명·맥락화하는 역할만** 하며, **AI가 꺼져 있어도 fact summary는 항상 존재**한다. lint 자체 룰셋은 두지 않는다.
- **approve:** access core와 **동일한 역할·승인 정책(`required_approvals`)·자기 승인 금지·payload 고정**을 재사용. migration plan(파일 집합+checksum+대상 connection)이 승인 payload.
- **effective class 선택:** migration 하나에 DDL·DML이 섞일 수 있으므로 모든 statement를 분류해 **가장 높은 등급(ddl > write > read)**을 effective class로 정해 그 `required_approvals`를 적용한다. unknown statement가 하나라도 있으면 승인 요청 생성을 거부한다.
  선택된 effective class와 connection policy version을 approval payload에 포함한다.
- **apply:** Atlas `migrate apply`.
  적용 시도와 결과는 audit event로 기록하고 자동 재시도하지 않는다(`outcome_unknown` 동일 규칙).
- **verify (revision verify):** apply 후 `migrate status`를 재조회해 **revision table이 목표 migration version에 도달했는지만** 확인한다. schema drift나 data postcondition은 검증하지 않는다(필요 시 향후 별도 postcondition check 추가).
- **audit:** 위 모든 스텝 전이를 access·query enablement와 **같은 통합 audit 타임라인**에 남긴다.
- **지원 범위:** PostgreSQL/MySQL별로 Atlas Community가 지원하는 object 목록을 compatibility matrix로 고정.
  각 DB가 독립된 contract test를 통과한 뒤 활성화하며 미지원 object는 status/dry-run 단계에서 명시적으로 거부.

### 4.6 Access Governance 확장 (Kviklet parity track)

**설계 원칙:** access governance의 비교 기준선은 **kviklet 0.9.2**로 맞춘다(ADR-0019). edition과 구현 단계를 구분하고, 동일 과업 검증으로 UX/UI와 query enablement·schema governance 통합의 가치를 확인한다.
기능별 기존 MVP/post-MVP/Later 경계를 유지하며 새 릴리스의 모든 기능을 alpha 필수 범위로 복제하지 않는다.

**최신 기준:** 0.7부터 결과 저장·transactional dry-run, 0.8부터 review sidebar·권한별 UI·요청 필터, 0.9부터 추가 필터·전체 cell 보기·사용자 비활성화·credential AEAD가 제공된다.
PostgreSQL/MySQL/MariaDB **proxy는 0.9부터 Enterprise-only(beta)**이며 웹 temporary access와 구분한다. role review gate·role sync도 Enterprise 기능이다. 0.9의 기본 활성화 telemetry는 비활성화할 수 있으며 instance URL·version 등을 전송한다.
Portcullis에 telemetry를 추가하는 결정은 하지 않는다. 0.9.2의 승인 명령 변경 및 결과 로그 노출 수정은 기존 immutable payload·실행 직전 재검증·결과 로그 금지 계약의 검증 시나리오에 반영한다.

| 기능 | 시점 | Portcullis 방침 |
|---|---|---|
| 단일 쿼리 요청·승인·반려 | MVP | PostgreSQL/MySQL 공통 지원 |
| connection별 RBAC·read/write/DDL 정책 | MVP | 기본 read-only, 자기 승인 금지 |
| 통합 audit log | MVP | query asset·schema change와 같은 timeline 사용 |
| 요청 댓글·review suggestion | post-MVP | 상태 전이와 분리된 append-only discussion으로 구현 |
| 임시 SQL 접근 session | post-MVP | **웹 SQL console session**으로 구현(서버 실행 경로 재사용 → dialect 무관·자동 정책·결과 그리드). **세션 중 모든 statement를 개별 audit event로 기록**(kviklet proxy가 `Connection.kt`에서 per-execute `saveEvent`하는 것과 동일 보장, 코드 검증 2026-06-27). threat model은 §4.9로 확정 |
| 다단계·role-based review gate | post-MVP | 정책 DSL보다 명시적 quorum/role rule부터 시작 |
| EXPLAIN | M2 (기본 Read 실행계획) | DB별 read safety와 `ANALYZE` 실행 여부를 분리 |
| Google 소셜 로그인(OIDC) | MVP | 서버 사이드 콜백 flow(프론트 SDK 없음). admin이 만든 사용자에 verified email로 링크, 자동 가입 없음(ADR-0007) |
| 그 외 OIDC provider/LDAP 및 group-role sync | post-MVP | 외부 IdP를 source of truth로 사용 |
| DB client용 proxy | Later (보류) | 임시 접근은 웹 console session으로 대체하므로 기본 미채택. native client(psql 등) 강한 수요가 검증되고 wire-level 정책·audit 완전성·credential 발급이 풀릴 때만 재고. kviklet 0.9는 PostgreSQL/MySQL/MariaDB를 지원하지만 Enterprise-only(beta); Portcullis도 dialect별 wire 구현·검증 비용을 별도로 평가 |
| API key | Later/검증 후 | 사용자 UI session과 분리된 scope·expiry·rotation 요구 |

Kubernetes exec, MongoDB/MSSQL, SAML/SCIM은 MVP와 parity track의 필수 범위가 아니며 실제 사용자 수요로 우선순위를 정한다.

### 4.7 이후 (Later)

- PostgreSQL/MySQL 외 추가 관리 대상 DB
- team 단위 공유·승인 정책
- **BI 분석·공유:** 저장 쿼리와 실행 결과를 차트·대시보드로 연결한다.
  - bar/line/pie 등 기본 시각화부터 시작하고 팀 분석 결과 공유로 확장한다.
  - 결과 접근 권한·보존·갱신 기준은 BI 마일스톤 착수 시 정의한다.
- declarative GitOps workflow (versioned migration git 연동은 Schema 마일스톤 4.5로 상향)
- CNPG 자동 발견 (Helm 배포 한정 옵션)
- SIEM 연동(audit webhook/JSON export)
- ML 이상 탐지 (audit 기반 위험 점수 → 4.8 AI Review와 연계)
- SAML/SCIM (수요 검증 후)
- **Agent Gateway 연동:** 에이전트가 Portcullis의 기능에 접근할 수 있는 연동 경로를 단계적으로 추가한다.
  - ADR-0028로 표준 로컬/원격 MCP Gateway를 조건부 M6(4.13)로 상향한다. 브라우저 WebMCP(4.10)는 선택 후속 adapter이며 초기 Gateway 선행 조건이 아니다.
  - 착수 시 에이전트 신원, 사용자 위임, 최소 권한, 승인 경계, 감사 추적을 정의한다.
  - Gateway 제품·프로토콜·인증 방식과 구현 순서는 수요 검증 후 ADR로 결정한다.

### 4.8 AI Review (옵션 value layer, post-MVP)

거버넌스의 진짜 비용은 사람 리뷰 시간과 리뷰어 전문성이다(성공지표 2.4의 승인 turnaround와 직결).
AI를 **승인 흐름의 보조 리뷰어**로 얹어 이를 줄인다.
코어 기능을 잠그지 않는 **옵션 레이어**이며, hosted AI는 향후 유료 후보(수익화 방향은 보류 — 별도 결정).

- **삽입 지점:** ① access request — SQL 요약·위험(full scan/PII/누락된 WHERE/write 범위) 신호·승인 근거 제안. ② schema migration(4.5) review 스텝 — deterministic fact summary를 **설명·맥락화**(평문 위험 설명·안전한 대안 online DDL/backfill 제안).
  단 영향 row·rewrite 같은 불확실 항목을 사실로 단정하지 않고 fact의 `unknown`을 존중. ③ audit `risk_score`(6.1) 채움.
- **가드레일 (필수):**
  - **Advisory-only.** AI 출력은 승인자에게 주는 입력일 뿐 `required_approvals`를 대체하거나 자동 승인하지 않는다.
    AI 사용 여부·결과는 audit에 기록.
  - **Opt-in + redaction.** 기본 비활성(8.5의 외부 telemetry off 원칙 준수).
    운영자가 명시적으로 켜고 provider를 선택한다. hosted provider에는 결과 row·평문 파라미터·credential·connection host를 전송하지 않고, SQL은 parser AST를 기준으로 comment를 제거하고 literal을 typed placeholder로 치환한다. schema/table/column identifier 전송도 별도 opt-in으로 둔다.
  - **Untrusted output + non-blocking.** SQL comment와 migration text를 지시문으로 신뢰하지 않으며 AI 출력은 escape된 text로만 렌더링하고 실행 가능한 SQL·정책으로 자동 적용하지 않는다. provider timeout·오류·quota 초과는 승인 상태 전이를 막지 않고 `review_unavailable` audit event만 남긴다.
  - **Provider-pluggable.** `AIReviewer` 인터페이스로 추상화(SchemaEngine·ResultStore와 동일 패턴).
    셀프호스트가 벤더에 묶이지 않게 하고, 셀프호스트 BYO-key는 무료·hosted는 유료로 분리 가능.
    인터페이스 형태(확정):

    ```go
    type AIReviewer interface {
        // 입력은 redactor(8.4)를 통과한 자료만: redacted SQL, statement class,
        // fact summary(4.5), 정책 컨텍스트. 결과 row·평문 파라미터·credential 불포함.
        ReviewAccessRequest(ctx context.Context, in AccessReviewInput) (Review, error)
        ReviewMigration(ctx context.Context, in MigrationReviewInput) (Review, error)
    }
    // Review{Summary string; Risks []RiskSignal; Suggestions []string; Provider, Model string}
    // RiskSignal{Kind string; Detail string; Confidence low|medium|high}
    // 오류·timeout·quota 초과는 Review 없이 error 반환 → 호출측이 review_unavailable 처리.
    ```

### 4.9 임시 접근(웹 SQL console session) 위협 모델 (M4 게이트, 2026-07-04 확정)

12.1에서 확정한 모델(웹 console session, 서버 실행 경로 재사용)의 세션 권한·만료·동시 실행·회수 규칙.
이 절이 M4 착수 게이트였던 "별도 PRD"를 대체한다.

- **부여(권한 범위):** 세션은 access request와 동일한 요청→승인 루프로 부여한다.
  승인 payload = connection + **허용 statement class 집합(read/write/ddl 부분집합)** + 세션 TTL + connection policy version.
  자기 승인 금지·`required_approvals`·승인 무효화 재검사(4.4)를 그대로 재사용하며, 세션 class 집합은 connection policy가 허용하는 class의 부분집합만 가능하다.
- **statement 게이트:** 세션 안에서도 **모든 statement가 개별적으로** 파서 분류(ADR-0002)를 통과하고 세션의 class 집합·connection policy와 대조된다.
  분류 불가·비허용 class·항상-거부 목록은 즉시 거부.
  "세션이 열려 있으니 통과"인 경로는 존재하지 않는다.
- **만료:** 세션 TTL 기본 **60분**, org 설정 범위 **15분~8시간**; **idle timeout 10분**(마지막 statement 종료 기준). *(수치 잠정 — M4 착수 시 재확인)* 만료 시 진행 중 statement는 driver cancel을 시도하고 결과 미확인이면 `outcome_unknown`(4.4 규칙).
- **동시 실행:** 세션당 동시 statement **1개**(순차 실행 — 터미널 모델과 일치, 정책 검사 경합 제거).
  사용자당 동시 활성 세션 **2개** 상한. *(잠정)*
- **회수(즉시 무효):** 사용자 비활성화, 세션 권한을 만든 승인의 무효화, connection policy 변경, connection archive, admin의 명시적 revoke — 어느 것이든 세션을 즉시 종료한다(진행 중 statement는 만료와 동일 처리).
  UI 세션(8.3)이 revoke되면 console 세션도 함께 revoke된다.
- **audit:** `SESSION_OPENED`/`SESSION_CLOSED`/`SESSION_REVOKED` + statement마다 6.1 불변식 그대로(`EXECUTION_STARTED`/`FINISHED`/`outcome_unknown`, redacted SQL + digest).
  "세션은 열리되 내역이 안 남는" 경로 금지(6.1).
- **transaction:** 기본 **stateless per-statement**(idle-in-transaction 차단, 12.1). stateful multi-statement transaction은 connection 정책 opt-in 후속 옵션으로, **read-only 한정 + transaction idle 60초 초과 시 자동 ROLLBACK** 경계를 여기서 확정한다. *(잠정)*

### 4.10 WebMCP 쿼리 보조 (M6 / Reach)

브라우저 WebMCP는 M5 완료와 쿼리·검토 API 안정화 이후 M6로 미룬다(ADR-0026, ADR-0024/0025 순서 변경). MVP 인수 범위에서 제외하며 PostgreSQL/MySQL parity와 사람이 사용하는 SQL 검토·미리보기를 우선한다. SQLite는 계속 제외한다. ADR-0028로 로컬/원격 MCP Gateway를 M6에 두며 브라우저 WebMCP는 선택 후속 adapter로 둔다. WebMCP 활성화에는 M4 마스킹 인수 통과와 M6의 인증된 에이전트 등록·권한 부여가 추가로 필요하다(ADR-0027). 등록을 연동보다 먼저 제공한다.

- 연결 탐색, 화면에 보이는 SQL·타입 파라미터 작성, 명시적 초안 저장·제출, 요청·승인 상태 조회, 요청자만의 승인된 실행, 제한된 결과 페이지 조회를 각각 도구로 제공한다. Read 쿼리부터 시작하며, 폼을 채우는 것만으로 자동 저장·실행하지 않는다.
- 스키마 탐색은 제한·인가·감사를 갖춘 catalog use case를 정의한 뒤 추가한다. 저장 쿼리 탐색·재사용은 Library가 해당 자산을 제공할 때 연동한다. 어느 경로도 임의 SQL 실행 권한을 부여하지 않는다.
- 현재 인증 사용자·조직과 기존 서버 권한, CSRF, 소유권, 별도 검토자, quorum, 불변 payload·config·policy, 1회 실행, 취소, audit, 결과 제한을 재사용한다. 초기 자동 승인·반려 도구는 제공하지 않는다. 실행은 명시적인 사용자 요청이 있어야 하며, 인증된 사용자 귀속과 신뢰할 수 없는 agent/source 표기를 구분한다.
- 일반 작업은 페이지 내부에 유지한다. 도구 결과와 직접 링크를 화면에 표시하고 로그아웃·신원·권한 변경·경로 종료 시 등록 해제와 진행 응답 fencing을 적용한다. 출력을 제한하고 catalog·SQL·결과 내용을 신뢰할 수 없는 데이터로 취급한다. 자격 증명·암호화 키·UI 쿠키를 노출하지 않는다.
- 네이티브 브라우저 지원을 감지하고 미지원 환경에서도 일반 UI를 사용할 수 있어야 한다. 착수 시 변경 중인 API를 다시 검증한다. 실제 지원 브라우저의 쿼리 과업, 거부·권한 회수·다른 사용자 접근, 재실행 거부, 취소, 정확하고 제한된 결과, 미지원 브라우저 fallback을 인수 조건으로 둔다. 가짜 registry만으로 네이티브 호환성을 입증하지 않는다.

### 4.11 SQL 검토와 스키마 미리보기 조기 제공 (M2–M3)

ADR-0026으로 에이전트 연동보다 검토 도구를 앞당긴다. M2는 MySQL parity 이후 statement 종류, 식별 가능한 참조 object, 적용 policy·limit을 결정론적으로 표시하고 미확인 항목은 unknown으로 둔다. 지원 Read 문장만 typed parameter와 서버 고정 옵션으로 기본 native EXPLAIN을 제공하며 ANALYZE, 위험 함수·연산자, 미분류 구문은 거부한다. 조직·connection 인가, archive 검사, target/config/policy 검증, 제한된 planning timeout·출력, 취소, 요청자만의 plan 접근과 audit가 필수다. 계획 조회는 승인이나 요청 실행이 아니다. SQL·parameter digest, target/config, 엔진 버전, 관측 시각을 근거에 연결하고 입력 변경 시 무효화하며 cost·row 수는 추정으로 표시한다. 두 DB 모두 parser 거절, 부작용 방어, 권한 거부·타 조직 접근, 오래된 입력, 민감 plan 출력 및 실제 엔진 시나리오 통과 후 제공한다.

M3는 4.5/ADR-0012의 고정된 불변 Git/Atlas artifact로 schema status → dry-run → 결정론 review만 제공한다. DB별 object matrix, 인가, audit, 제한된 subprocess·catalog 작업을 강제한다. fact 산출원·관측 시각·추정·unknown을 표시한다. M5 전에는 migration apply endpoint를 제공하지 않으며 미리보기는 변경 시뮬레이션이나 rollback·lock 안전성 보장이 아니다. 실제 apply 전 artifact와 target 상태를 재검증한다.

EXPLAIN ANALYZE, 실행 후 rollback하는 일반 쿼리 dry-run, AI review, 인덱스 추천은 후속 범위로 유지한다. 기본 검토 정보는 정확한 영향 row 수나 위험 점수를 약속하지 않는다.

### 4.12 민감정보 마스킹과 등록된 에이전트 (M4 → M6)

M4에서 서버 마스킹·출력 보류를 먼저 제공하고 M6에서 에이전트 등록 후 연동한다(ADR-0027). 기존 SQL audit redaction은 결과 데이터 마스킹이 아니다. admin이 버전별 조직·connection 공개 규칙을 관리하고 API·cell·CSV·SQL/catalog/plan/review metadata·후속 tool 출력 직렬화 전에 적용한다. 처음에는 값 전체 가리기·필드 제외부터 제공한다. 요청자 소유권은 마스킹을 우회하지 않으며 사람의 원문 예외는 별도 명시 권한과 audit가 필요하다. 초기 에이전트에는 원문 예외가 없다. 승인 SQL·parameter, 실행 의미, 암호화 원본과 기존 보존 정책은 유지한다.

에이전트 출력은 기본 거부하며 명시 허용된 보호 필드만 제공한다. secret·credential·원문 SQL/parameter·민감 cell 원문은 보내지 않는다. 출처 불명·alias·expression, 검증되지 않은 free-text/JSON·미지원 encoding은 보류하고 마스킹 오류는 안전하게 거부한다. 매 조회·export·응답에서 현재 정책을 재검증하고 규칙 변경 시 준비된 export·오래된 변환 cache를 무효화한다. 숨긴 값을 유추할 수 있는 sort/filter/search는 제한한다. audit에는 정책 버전·판단만 남기고 민감 값은 남기지 않는다. 자동 탐지는 정책 설정 보조이며 인가 근거가 아니다. UI/API/CSV/full-cell 일치, metadata/plan/error 누출, 파생 값, 오래된 cache, 실패, 조직 격리와 두 DB의 canary-secret 시나리오를 인수 조건으로 둔다.

마스킹 통과 후 조직 admin이 stable ID·owner·연동 종류·pending/disabled/active/revoked 상태·허용 tool/connection·보호 출력 정책·만료가 있는 사용자 위임을 가진 에이전트를 등록한다. 등록 시 권한은 없고 활성화는 명시적이다. 변경·사용을 audit한다. 실제 권한은 인증 사용자·검증된 agent grant·조직/connection·마스킹 정책의 교집합이다. 제출한 이름/ID는 신뢰하지 않는다. 회수·만료·로그아웃 시 후속 호출을 거부하고 진행 응답도 fence한다. 명시적 사용자 실행 요청·별도 검토·quorum·payload 무결성·1회 실행을 유지하며 자동 승인/반려 tool은 제공하지 않는다.

연동 전 transport ADR로 호출자를 활성 등록·위임에 결합하는 방법을 입증한다. native WebMCP만으로 에이전트를 인증할 수 없으며 신원을 확인하지 못하면 보호 기능을 거부한다. 등록은 실행 plugin 설치나 임의 endpoint fetch가 아니다. M6 MCP Gateway 인가는 browser token 전달 없이 4.13/ADR-0028을 따른다. 위조 ID·타 조직·회수/만료 grant·권한 교집합·보호 출력이 인수 조건이다. 이는 Portcullis 경유 공개를 통제하며 외부 에이전트의 독립적인 browser/DOM 접근을 통제한다고 약속하지 않는다.

### 4.13 에이전트 중립 MCP Gateway와 배포 (M6)

M4 마스킹과 M6 등록·권한 인수 통과 후 Streamable HTTP 표준 MCP Gateway와 같은 인증 endpoint로 연결하는 로컬 stdio bridge를 opt-in 제공한다(ADR-0028). vendor SDK 종속 없이 로컬 일반 client·Claude Code·Codex를 대상으로 한다. 기록된 client 버전·협상 protocol·인가·실제 거버넌스 tool 과업 통과 후에만 지원으로 표시한다. 브라우저 WebMCP는 선택 후속이며 초기 Gateway 인수 조건이 아니다. Go 단일 binary의 선택 adapter/subcommand와 기존 app port를 유지한다. agent/model 실행·hosting, 임의 MCP server proxy, bridge의 대상 DB 직접 연결은 제공하지 않는다.

Gateway는 요청마다 인증 issuer/audience/expiry/scope·활성 등록·만료가 있는 위임을 검증하고 사용자/조직/connection/tool/마스킹 권한의 교집합을 적용한다. 표준 HTTP 인가·resource discovery는 검증된 호환 provider를 사용하며 외부 배포할 수 있다. 등록은 OAuth client 등록과 다르며 암묵 권한을 부여하지 않는다. 비인증 local 모드, cookie/token 전달, agent 이름 header 신뢰는 금지한다. stdio stdout은 protocol 전용, stderr는 정제하며 credential은 제한된 범위로 예제·로그에서 제외한다. local HTTP는 기본 loopback, 배포 시 TLS/origin 제어가 필요하다. 명시적 사용자 의도·별도 승인·불변 payload·1회 lease·제한된 보호 출력·취소·audit를 유지한다.

인프라는 TLS/ingress·provider hosting·Secret 전달/rotation·network policy·관측 배포를 맡고 Portcullis는 신원과 업무 정책 검증을 유지한다. endpoint·인가 discovery routing, Secret 참조, probe, resource/body/stream limit과 네트워크 제한의 Helm/Kustomize 예제를 제공한다. NetworkPolicy에는 적용 plugin이, Secret에는 보호된 저장·접근이 필요하다. Kubernetes routing/discovery·rotation·proxy buffering/timeout·재시작/종료·우회 차단을 검증한다. multi-replica 주장은 pod-local grant가 아닌 공유 등록/회수 상태와 1회 실행 테스트 통과 후 가능하다.

실제 로컬/Claude Code/Codex client matrix, protocol 호환, auth discovery, 거부/위조/타 조직/만료/회수 접근, masking canary, 연결 끊김/취소·재실행 거부를 인수 조건으로 둔다. 정확한 auth provider와 protocol 구현은 착수 전 후속 ADR로 정한다. 계획된 M6이며 MVP 범위 밖이다.

---

## 5. 기술 아키텍처

### 5.1 스택 선택

| 영역 | 선택 | 비고 |
|---|---|---|
| 언어 | Go | provider와 언어 공유, 단일 바이너리, 작은 공격표면 |
| 라우터 | `net/http` (Go 1.22+) | 의존성 최소, `GET /x/{id}` 라우팅 내장 |
| API 전송 | **Connect RPC** (protobuf) | `connect-go`는 `net/http` 기반. protobuf 단일 명세로 Go 서버 + TS 클라이언트 생성, end-to-end 타입 안전 |
| 실시간 | **Connect server-streaming** | migration 워크플로 라이브 뷰·승인 알림·(향후)임시 세션 모니터링을 한 메커니즘으로. SSE/WebSocket 불필요. stream은 끊길 수 있으므로 **재연결 시 현재 상태를 unary로 다시 조회한 뒤 stream을 재구독**하는 recovery 규칙을 둔다 |
| 메타데이터 DB | PostgreSQL | MVP compose=컨테이너, 외부 PG/Helm은 이후 |
| 메타데이터 DB 접근 | `sqlc` on `pgx` | raw SQL + 타입 안전. ORM 미사용 |
| 관리 대상 DB 접근 | dialect adapter + native driver | PostgreSQL/MySQL 차이를 명시적으로 격리 |
| 인증 | password(argon2id) + Google OIDC + 서버사이드 세션 | `coreos/go-oidc` + `x/oauth2`, 서버사이드 콜백(프론트 SDK 없음). 그 외 OIDC/SAML은 이후 |
| Authz | Go 레이어(RBAC + org 스코프) | repository 강제 + cross-org 통합 테스트. metadata RLS는 MVP 미적용으로 확정(ADR-0004, 스키마는 RLS-ready) |
| 프론트엔드 | SolidJS SPA + Vite | CSR. `go:embed`로 바이너리에 포함 |
| UI 라이브러리 | Kobalte + Tailwind + TanStack Table | 데이터 그리드가 제품 핵심 |
| Schema 엔진 | Atlas Community CLI subprocess | 버전·checksum을 고정하고 `SchemaEngine` 인터페이스로 격리 |

### 5.2 메타데이터 저장소 원칙
**"어디서 돌든 메타데이터는 PostgreSQL."** compose든 Helm이든 동일 스키마·쿼리·sqlc 코드가 동작.
환경별로 저장소가 갈리면 코드가 두 벌이 되므로 금지.
(etcd 등 KV 스토어를 메타데이터 저장소로 쓰지 않음.)

### 5.3 DB dialect 통합 경계

Core 1/2의 상위 서비스가 DB별 placeholder, parser AST, transaction 차이를 직접 분기하지 않도록 adapter로 격리한다.

```go
type QueryDialect interface {
    ParseSingle(sql string) (Statement, error)
    Classify(stmt Statement) (StatementClass, error)
    BindNamed(sql string, params []TypedValue) (boundSQL string, args []any, err error)
    ValidateConnection(ctx context.Context, cfg ConnectionConfig) error
    Execute(ctx context.Context, req ExecutionRequest) (ResultStream, error)
}
// postgresDialect, mysqlDialect
```

- 공통 service가 payload digest, approval, execution lease, timeout, row/byte cap, result snapshot, audit을 담당.
- adapter는 연결 검증, 정확한 statement 분류, bind 문법, read-only/transaction 설정, cancel과 오류 redaction을 담당.
- DB별 구현은 동일한 contract test suite를 통과해야 하며 의도적인 차이는 compatibility matrix와 UI에 노출.
- driver와 parser 라이브러리는 ADR-0001로 확정: pgx / go-sql-driver/mysql + per-dialect parser(PG=pgplex/pgparser, MySQL=tidb pkg/parser). ADR-0025로 SQLite를 지원 범위에서 제외한다.

### 5.4 Atlas 통합 경계

추상화 레이어로 Atlas를 감싼다.
상위 코드는 도메인 타입만 알고, Atlas는 한 구현체 뒤에만 존재한다.

```go
type SchemaEngine interface {
    Status(ctx, conn, source Source) (MigrationStatus, error)  // applied vs pending
    DryRun(ctx, conn, plan Plan) (Preview, error)              // pending SQL preview
    Apply(ctx, conn, plan Plan) (ApplyResult, error)
}
// v1: atlasSubprocessEngine (Community 바이너리 호출 — migrate status/dry-run/apply)
```

`Source`는 **remote ID + commit SHA + path + 순서 고정된 파일 목록·파일별 checksum + `atlas.sum` + Atlas version/options + immutable artifact handle**을 담는다(4.5 승인 artifact와 동일).
`Plan`은 그 artifact에서 확정된 적용 대상 migration 집합을 가리킨다. review 스텝의 **deterministic fact summary**는 `Preview`(dry-run SQL) + DB native 분석으로 만들고, AI Review(4.8)는 이를 설명만 하며 불확실 항목은 `unknown`으로 둔다(영향 row를 단정하지 않음).
Atlas에 자체 lint/pre-check를 의존하거나 추가하지 않는다.
Go embedded 전환은 Community 라이선스와 공개 API 안정성을 별도 검증한 뒤 결정하며 현재 로드맵에 약속하지 않는다.

### 5.5 라이선스 지도

| 기능 | 출처 | 비고 |
|---|---|---|
| versioned migration status / apply / dry-run | Atlas Community (Apache 2.0) | 바이너리 번들 가능; 버전·checksum 고정 |
| git source 연동·migration plan 저장·승인 | **자체 구현** | Atlas Pro `schema plan`에 의존하지 않음 |
| migration lint (위험 탐지) | **미구현(범위 제외)** | 자체 lint 룰셋을 두지 않음. 위험 판단은 review 스텝(사람+AI 4.8) |
| pre-migration check | **미구현(범위 제외)** | assertion 게이트를 두지 않음. review 스텝으로 대체 |
| migration review (deterministic fact summary) | **자체 구현 + optional AI(4.8)** | dry-run SQL + DB native 분석 기반. 불확실 항목은 `unknown`, AI는 설명만 |
| EXPLAIN (쿼리 실행계획) | **자체 구현** | DB 네이티브 `EXPLAIN`, Atlas 무관 |
| audit / 승인 / 배포 이력 | **자체 구현** | 제품 본체 (Atlas는 Cloud 유료) |

Atlas Community에서 제외되는 declarative plan, migration lint, pre-check, approval policy, Go SDK와 고급 DB object는 사용하지 않으며, 그에 대응하는 자체 lint/pre-check도 만들지 않는다(차별화는 git 연동 + 거버넌스 + review + UX).
Atlas 버전 변경 시 compatibility suite와 라이선스 지도를 함께 갱신한다.

### 5.6 멀티테넌시 전략
- 모든 핵심 테이블에 `organization_id` 컬럼.
  셀프호스트에선 `default-org` 단일.
- 모든 쿼리는 repository 레이어에서 org 스코프를 강제 통과.
- ID만 바꾼 API 요청으로 다른 org 데이터에 접근할 수 없는지 endpoint별 통합 테스트.
- DB 격리(스키마/DB 분리)는 클라우드 전환 시 결정.
  MVP는 shared-table + org_id.
- RLS 미사용은 “애플리케이션이 유일한 데이터 경로”라는 위협 모델과 운영 제약을 근거로 확정됨(ADR-0004); 멀티테넌트 SaaS 착수 시 재검토.

---

## 6. 데이터 모델 (개요)

```
organizations            (셀프호스트: default 1개)
users                    (계정)
organization_memberships (user-org; role_id → roles)
permissions              (Google-IAM 스타일 resource.verb 카탈로그; SQL seed, 시작 시 로드)
roles                    (org-scoped; name, is_system; 시드 3개 default + custom role)
role_permissions         (role-permission 할당; permission_key → permissions)
auth_methods             (password; user와 분리해 인증수단 확장 대비)
oidc_identities          (user-issuer-subject 링크; Google 소셜 로그인, unique(issuer,subject))
sessions                 (opaque token hash, idle/absolute expiry, revoked_at)
oidc_providers           (엔터프라이즈용, 나중)

connections              (PostgreSQL|MySQL; encrypted config, org_id, current_policy_version, archived_at)
connection_policy_versions (connection, version, read/write/ddl별 required_approvals + policy당 limit 1세트(timeout/rows/bytes — ADR-0015), created_by)
access_requests          (AEAD-encrypted SQL+params payload, payload_digest, redacted_sql, statement_class, policy_version, required_approvals, 상태, expires_at)
                         — redacted_sql의 원본은 이 행이며, audit event는 기록 시점 값을 **복사**해 적재한다
                           (참조 아님: append-only 감사는 자기완결이어야 하고, 이후 행 상태 변화와 무관해야 함)
approvals                (request, approver, decision, reason, decided_at; UNIQUE(request, approver))
query_executions         (request_id unique, lease owner/deadline/heartbeat, attempt_id, outcome, result metadata)
saved_queries            (name, tags, visibility, owner, source_request_id?; 입력 한도는 4.2)
saved_query_versions     (immutable AEAD-encrypted sql + parameter default values, parameter definitions, created_by)
saved_query_favorites    (user별 즐겨찾기)
schema_change_requests   (Schema 마일스톤; commit SHA, artifact handle, plan hash, atlas.sum,
                          Atlas version/options, effective statement class + policy version,
                          migration lock/attempt/outcome, artifact retention class, review summary, 상태)
audit_events             (통합 감사 타임라인; ML feature-ready 구조)
```

`result_set` 스냅샷은 in-process cache 대신 `ResultStore` 인터페이스 뒤의 PostgreSQL `result_cache` schema에 보관한다.
`result_sets`와 `result_chunks`는 **UNLOGGED table**이며, 각 result마다 생성한 data-encryption key(DEK)로 schema/row chunk를 AES-256-GCM 암호화하고 DEK는 master key로 wrapping한다.
각 chunk는 result ID·chunk index·owner organization ID를 associated data로 인증한다.
평문으로 남는 필드는 result ID, owner organization/user, row·byte 수, 생성·만료·최근 접근 시각뿐이다.
`query_executions`에는 handle, 만료 시각, row/byte 수, truncated 여부만 기록하고 UNLOGGED table을 FK로 참조하지 않는다.

UNLOGGED result cache는 같은 PostgreSQL primary에 연결된 여러 Portcullis replica가 공유할 수 있지만 crash recovery와 standby 복제 대상은 아니다.
PostgreSQL crash·failover로 cache가 사라지면 실행 결과 자체를 재실행하지 않고 UI에 `result_unavailable`을 표시하며, 영속적인 `query_executions` 성공 기록은 유지한다.
이 손실 모델은 15분 TTL cache에 대해 의도적으로 수용한다.

### 6.1 Audit 이벤트 (ML-ready 설계)
나중에 ML 이상 탐지를 붙일 수 있도록 처음부터 구조화된 필드로 적재한다.

```
audit_events
  id, organization_id, occurred_at
  actor_type(user|system|service), actor_user_id(nullable), actor_service(nullable)
  action, target_type, target_id, outcome
  previous_state, next_state, payload_digest, request_id
  connection_id, query_type, rows_affected, duration_ms
  risk_score(nullable)                       -- 나중에 채움(ML/AI Review 4.8)
  metadata(jsonb)
```

- runtime DB role은 `audit_events`에 `INSERT/SELECT`만 가능하고 `UPDATE/DELETE`할 수 없음. schema migration용 owner role과 분리.
- **actor 모델:** 사람이 아닌 행위자(자동 승인 `required_approvals=0`, reconciler, AI review, late-completion)는 `actor_type=system|service`로 기록한다.
  `actor_user_id`는 nullable이고, system/service actor는 `actor_service`에 **안정적인 식별자**(예: `system:reconciler`, `service:ai-review`)를 남긴다.
- **statement 단위 로깅(불변식):** 실행 경로가 무엇이든 — web 직접 실행, 향후 임시 접근 session(4.6) — **대상 실행에 진입하는 모든 시도에는 `EXECUTION_STARTED` audit event가 존재**하고, 결과 확인 시 `EXECUTION_FINISHED`(terminal), 결과 미확인 시 `outcome_unknown`이 기록된다(crash 구간에 "DB 도달" 자체는 증명할 수 없으므로 "진입한 시도에는 STARTED가 있다"가 보장 단위).
  소유자가 확인된 요청의 실행 전 거부(무결성·상태·정책·포화·종료 중 포함)는 `EXECUTION_REJECTED`로 기록하며 lease를 소비하지 않는다(ADR-0021).
  기록되는 쿼리 텍스트는 **comment 제거 + inline literal→typed placeholder + bind placeholder 유지로 만든 redacted SQL + payload digest**이며(원문 SQL은 8.4대로 암호화 저장, audit엔 미기록, redaction 실패 시 fail-closed로 digest+type만) 평문 파라미터 값·literal·comment·결과 row는 남기지 않고 query_type·rows_affected·duration만 남긴다.
  임시 접근을 도입하더라도 "세션은 열어주되 실행 내역은 안 남는" 경로를 만들지 않는다.
  (kviklet proxy `Connection.kt`가 per-execute로 `saveEvent`하는 것과 동일 보장 — 코드 검증 2026-06-27)
- audit에는 credential, session token, query 결과 row를 저장하지 않음.
  파라미터 값은 request payload에 암호화하고 audit에는 payload digest만 기록.
- **내역 보존:** access_request·query_execution·audit_event는 connection archive 시 그대로 남는다(4.3).
  MVP는 connection hard delete API를 제공하지 않고 FK는 `ON DELETE RESTRICT`로 보호한다. audit event에는 credential을 제외한 connection 식별 snapshot을 함께 적재한다.
- self-host DB owner의 직접 변조까지 막는 cryptographic tamper evidence는 Later 범위이며, MVP는 이 한계를 문서에 명시.
  MVP 변조 방지의 기준선은 append-only + runtime의 UPDATE/DELETE 차단이다.

---

## 7. 핵심 UX 요구사항

### 7.1 결과 그리드 & 페이지네이션

일반적인 작성·수정·요청 검토·정책 설정·결과 조회는 페이지 내부에서 제공하며, 모달 확인은 위험하거나 파괴적인 작업에 한정한다(ADR-0022). 요청 작성·상세·결과는 직접 새로고침 가능한 URL과 브라우저 이력, 명시적인 돌아가기 링크를 제공한다. 백그라운드 갱신 중 입력한 SQL과 결정 사유를 보존하고 Save draft/Submit을 명시적으로 실행해야 한다. 경로 이동이 평문을 자동 저장하거나 SQL을 실행해서는 안 된다.

편집 가능한 SQL은 기본적으로 입력칸을 벗어날 때 로컬에서 자동 정렬하며, 자동 정렬 끄기·수동 정렬·되돌리기를 제공한다(ADR-0023). 대소문자와 파라미터 표기를 보존하고 정렬 실패 시 입력을 유지한다. 제출된 SQL은 저장된 원문으로 표시하며 승인 증거와 실행 입력을 포매터가 변경하지 않는다.

감사 맥락에서 명시적인 탐색 위치와 동일 실행 결과의 안정적인 조회를 제공한다. kviklet도 페이지 조회·요청 필터·결과 저장·전체 cell 보기를 제공하므로, 페이지네이션 유무를 차별점으로 주장하지 않고 실제 과업으로 UX를 비교한다(ADR-0019).

- **목록(요청/저장쿼리/audit):** OFFSET 페이지네이션 채택(내부 백오피스 성격에 적합).
  - `page`, `page_size`(10/20/50/100, 기본 20, **상한 100**), `sort`(컬럼 화이트리스트 + 방향).
  - 응답: `{ items, page, page_size, total_count, total_pages }`.
  - **명시적 페이지 컨트롤**(번호 점프, "1–20 of 1,340").
    무한스크롤 미사용 — 감사 맥락에서 "전체 중 몇 페이지"의 통제감이 중요.
  - 정렬은 **tie-breaker 포함**(`ORDER BY created_at DESC, id DESC`)으로 페이지 경계 안정성 보장.
  - audit_event 등 폭증 테이블은 추후 필요 시 keyset으로 국소 전환(공통 list 헬퍼를 인터페이스로).
- **쿼리 결과:** 실행 1회 후 서버가 snapshot을 **PostgreSQL UNLOGGED result store(AEAD 암호화 chunk)**에 15분 TTL로 보관하고 그 위에서 페이징/정렬/필터(DB 재실행 없음).
  - 최대 10,000행과 25MiB 중 먼저 도달한 snapshot 상한에서 중단하고 `truncated=true` 표시.
    connection 정책의 더 낮은 상한을 우선 적용한다(신규 정책 기본 byte 상한 16MiB, ADR-0015/0021).
    디코드 전에 셀·행 구조체 메모리를 별도로 제한하므로 값 바이트가 작은 넓은 NULL 결과도 더 일찍 truncate될 수 있다(ADR-0021).
  - 전체 result store 상한은 기본 512MiB이며 LRU로 만료.
    사용자별 quota(기본 64MiB)와 축출→거부 우선순위는 ADR-0011로 확정.
    TTL 만료·상한 축출 시 UI에 만료 상태 표시.
  - 원래 순서의 페이지 조회와 CSV는 필요한 chunk만 순차 복호화한다.
    CSV는 전체 snapshot을 메모리에 올리지 않고 stream한다.
  - 정렬·필터는 암호문을 PostgreSQL에서 질의하지 않는다.
    서버의 제한된 processing worker가 최대 25MiB snapshot을 요청 처리 동안만 복호화해 수행하고 결과 page만 반환한다. worker 동시성 기본값은 2이며 포화 시 `429 Retry-After`로 backpressure를 적용한다.
  - **상주 in-process cache 비채택:** 전체 결과를 Go heap에 TTL 동안 보관하지 않는다.
    정렬·필터 작업의 일시 메모리는 row/byte 상한과 worker semaphore로 제한한다.
  - 같은 primary를 사용하는 application replica 간에는 공유되지만 PostgreSQL crash·standby failover 시 cache가 사라질 수 있다.
    이때 재실행하지 않고 `result_unavailable`을 표시한다.
  - CSV export도 동일한 snapshot과 상한을 사용하며 매 요청마다 원래 사용자/organization 권한을 다시 검사.
  - **CSV injection 방어:** `=,+,-,@`(및 tab/CR 선행) 으로 시작하는 cell은 기본 escape해 spreadsheet formula 실행을 막는다. raw export가 필요하면 명시적 옵션 + 경고로만 허용. bytes/JSON/newline/encoding 직렬화 규칙은 result 타입 계약(12.2)을 따른다.
  - 무제한 export와 query 재실행 기반 export는 MVP에서 지원하지 않음.

### 7.2 Connection 추가 플로우
- 타입별 폼을 제공하고 사용하지 않는 필드는 숨김.
  - PostgreSQL: host/port/database/user/password/TLS mode.
  - MySQL: host/port/database/user/password/TLS mode.
- 공통 descriptor 필드: **environment**(development|production — UI는 production을 명시적 배지로 표시)와 **description**(선택, ≤500자) (2026-07-18 증보).
- 저장 전 연결 테스트.
- admin만 생성·수정·테스트 가능하며 credential과 원문 DSN은 생성 후 UI/API로 다시 반환하지 않음.

### 7.3 Migration 워크플로우 뷰 (Schema 마일스톤)
- **Argo Workflow처럼** 각 스텝을 시각화한 수평 플로우(선형). 6스텝: status → dry-run → review → approve → apply → verify.
- 각 스텝: 상태색 아이콘 + 이름 + 소요시간, 클릭 시 상세 펼침 — status(applied/pending), dry-run(pending SQL), review(deterministic fact summary·unknown 표기), apply/verify(로그).
- approve 스텝에서 정지 → 노란색 + 승인/반려 버튼. access core와 동일한 승인 정책·역할 사용.
- 실시간 상태 업데이트는 Connect server-streaming으로 푸시(별도 SSE/WebSocket 불필요).
- 워크플로우 엔진(Argo/Temporal 등) **미사용** — 6스텝 고정 상태 머신을 직접 모델링하고 UI만 Argo식 워크플로우처럼 렌더링.

### 7.4 승인 알림 (MVP 최소 범위)
- 승인 turnaround는 성공지표(2.4: `pending→approved|rejected` 중앙값 30분 이하)에 직결되므로 **in-app 알림이 MVP 범위**다.
- approver에게는 pending 요청 수 badge와 목록, requester에게는 승인/반려/만료 상태 변화 표시.
  Connect server-streaming(또는 폴백 폴링)으로 갱신.
- 이메일·Slack 등 외부 채널 알림은 post-MVP(외부 발송 인프라·설정과 함께). 2.2의 "알림 엔진" 비목표는 BI성 알림을 가리키며 이 승인 알림과 구분된다.

---

## 8. 보안 & 운영 요구사항

### 8.1 Secret과 connection 보호
- connection credential과 access request parameter values는 versioned envelope format의 AES-256-GCM으로 암호화해 저장한다. record마다 CSPRNG nonce를 생성하고 **canonical AAD `portcullis/aad/v1|<record_type>|<organization_id>|<record_id>[|<chunk_index>]`**(ADR-0003)를 associated data로 인증해 ciphertext 교체를 막는다. key version은 AAD에 넣지 않고 HKDF 파생 wrap key 선택으로 묶인다(버전 변조 = 복호 실패). 32-byte master key가 없거나 형식이 잘못되면 서버 시작을 거부.
- production에서는 master key를 환경변수 평문보다 mounted secret으로 주입하도록 문서와 Compose 예제를 제공. key ID를 함께 저장해 재암호화 기반 rotation이 가능해야 함.
- 새 PostgreSQL/MySQL connection은 인증서를 검증하는 TLS mode가 기본.
  완화된 TLS 설정은 admin의 명시적 선택과 audit event가 필요.
- API·로그·audit에서 password, 원문 DSN, session token, 암호화 전 parameter values, result row를 노출하지 않음.
  대상 DB 오류도 credential을 redaction한 뒤 반환.
- 관리 대상 DB 계정은 connection 정책에 맞는 최소 권한 계정을 사용하도록 setup guide와 connection test 경고를 제공.
- **Git 소스 보안(Schema 마일스톤):** git credential은 connection credential과 동일하게 AEAD로 암호화 저장하고 로그에서 redaction.
  허용 Git host·outbound allowlist를 강제하고 **redirect 제한과 DNS rebinding 방어**를 둔다. **clone 크기·파일 수·timeout 상한**을 적용하고, **submodule·Git LFS·symlink는 기본 금지**한다. fetch한 migration 파일은 승인 artifact(4.5)로 고정해 재읽기 없이 apply하며, artifact의 **보존 기간과 접근 권한**을 정의한다.

### 8.2 SQL 실행 안전성
- DB dialect parser로 정확히 하나의 statement인지 검증하고 statement 종류를 connection policy와 대조.
  단순 keyword/정규식만으로 권한을 판단하지 않음.
- 명명된 파라미터는 각 native driver의 bind parameter로 변환.
  값은 SQL 문자열에 직접 삽입하지 않음.
- PostgreSQL 실행은 선언된 파라미터 타입을 native OID로 전달한다(ADR-0016).
  `timestamp`는 RFC 3339 시점이므로 `timestamptz`에 대응한다.
  `null`에는 기반 타입이 없으므로 SQL 문맥에서 추론하고, 모호한 표현식은 명시적 cast를 요구한다.
- 각 실행은 전용 DB connection을 사용하고 DB가 지원하는 범위에서 transaction과 read-only mode를 강제.
  PostgreSQL/MySQL의 implicit commit, timeout, cancel 차이는 adapter contract와 승인 UI에 명시.
- **read-only 트랜잭션은 함수 부작용을 막지 못한다 (2026-07-24 증보, ADR-0002):** PostgreSQL의 `READ ONLY`는 문서상 "a high-level notion of read-only that does not prevent all writes to disk"로, 금지 대상은 명령(INSERT/UPDATE/DELETE/MERGE/COPY FROM/DDL/GRANT/TRUNCATE)뿐이다.
  따라서 `dblink_exec`·`pg_notify`·`set_config`·advisory lock·서버 파일 함수는 `SELECT` 안에서 통과한다.
  방어는 ① 분류 시점 **함수·연산자 allow-list** — 목록 밖·사용자 정의·스키마 수식 이름은 fail-closed 거부이고, **클래스와 무관하게 ddl 포함 모든 문장에 적용**한다(2026-07-25 증보: ddl은 등급만 최종이고 부작용 검사를 면제하지 않는다 — CTAS·표현식 인덱스·컬럼 DEFAULT가 함수를 품는다) ② 실행 시 명시적으로 참조된 함수·연산자의 **후보 OID 전체를 검증**(고정 `search_path` + 신뢰 카탈로그 대조; 사용자 overload가 하나라도 있으면 거부하는 보수적 대안, ADR-0021) ③ 대상 DB 계정 최소권한(§8.1)이다.
  `pg_proc.provolatile`은 **경계가 아니다**(2026-07-25 정정): PG 문서는 volatility를 *"a promise to the optimizer"* 로 규정하고 *"not a completely bulletproof test, since such functions could still call VOLATILE functions that modify the database"* 라고 명시한다 — 서버가 강제하지 않으므로 함수 생성 권한자는 부작용 있는 본문을 STABLE로 선언할 수 있다.
  정직하게 선언된 volatile builtin을 걸러내는 **위생 검사**로만 남긴다. read-only 트랜잭션도 경계가 아니라 보조 수단이다.
- row 상한뿐 아니라 byte 상한을 강제해 큰 cell에 의한 메모리 고갈을 방지. cache 상한 도달 시의 처리 순서는 확정됨(ADR-0011): 만료분 삭제 → 본인 LRU 축출 → 전역 LRU 축출(사용자별 최소 1개 보존) → 그래도 부족하면 신규 snapshot만 거부(`result_store_full`, 실행 자체는 완료).
- **대상 DB 실행 경로 circuit breaker(ADR-0010):** connection별로 연속 실패 5회 초과 시 60초 open(half-open probe 1회).
  차단된 호출은 lease를 잡지 않고 `Unavailable`로 반환하며 자동 재시도하지 않는다.
- 사용자 cancel과 context timeout 시 driver cancel을 시도하지만, 결과를 확인할 수 없으면 성공/실패를 추측하지 않고 `outcome_unknown`으로 기록.
- 대상 DB 실행에는 자동 retry를 적용하지 않음.
  API idempotency key는 동일 실행 attempt 조회에만 사용하고 새 DB 실행을 만들지 않음.

| DB | read 실행 | write/DDL 실행 시 주의사항 |
|---|---|---|
| PostgreSQL | read-only transaction | 가능한 statement는 transaction 안에서 commit/rollback |
| MySQL | read-only transaction | DML은 transaction 사용, DDL implicit commit 가능성을 실행 전 경고 |

`COPY ... PROGRAM`, `SELECT ... INTO OUTFILE`, `LOAD DATA`, `ATTACH/DETACH`, 쓰기 가능한 `PRAGMA`처럼 서버 파일·네트워크·세션 상태에 영향을 주는 문장은 별도 안전 설계 전까지 거부한다.

### 8.3 인증과 세션
- password hash에는 versioned argon2id 파라미터를 저장하고 기준이 바뀌면 로그인 시 rehash.
- session token은 원문을 저장하지 않고 hash만 저장.
  로그인·권한 상승 시 rotation, 로그아웃/admin revoke 즉시 무효화.
- 열린 Connect server-stream은 session 무효화 이후에도 살아남을 수 있으므로 **최대 stream 수명 30분**(클라이언트가 조용히 재구독), **60초 주기 session 재검증**, 사용자 비활성화·session revoke 시 강제 종료를 적용한다.
  알림 폴백 폴링 주기는 **30초**(7.4).
  (ADR-0010)
- session token은 CSPRNG 32바이트(opaque)이고 cookie는 `__Host-` prefix + `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`.
  상태 변경 요청은 **HMAC 서명·세션 바인딩된 double-submit CSRF token**(읽기 가능한 `__Host-` CSRF cookie ↔ `X-CSRF-Token` 헤더 일치 + HMAC 검증)을 검증한다. naive double-submit은 우회 가능하므로 쓰지 않는다.
  (ADR-0006)
- 기본 idle expiry 12시간, absolute expiry 7일.
  로그인 endpoint에는 IP와 계정 기준 rate limit(토큰버킷 수치는 ADR-0010) 및 점진적 backoff(계정 연계 실패 카운터·lockout 수치는 ADR-0006 Parameters) 적용.
- bootstrap admin 생성은 사용자가 없는 최초 1회로 제한하고 완료 후 bootstrap 경로를 비활성화. **config 기반 부트스트랩 (2026-07-23 증보, ADR-0006):** 대화형 `/bootstrap` 폼 외에 `PORTCULLIS_BOOTSTRAP_ADMIN_EMAIL` + 정확히 한 개의 비밀번호 소스(`_PASSWORD` 또는 마운트 시크릿 `_PASSWORD_FILE`; 선택 `_DISPLAY_NAME`, 기본 `Admin`)로도 최초 admin을 만들 수 있다.
  서버는 사용자가 0명일 때만 부팅 중 같은 Bootstrap 유스케이스를 실행하고(락 하 재검사로 대화형과 race-safe), 사용자가 있으면 로그만 남기고 건너뛴다.
  부분 설정·약한 비밀번호는 **기동 거부**(master key와 동일 fail-fast).
  표시 이름(display name)은 이제 **필수**다 — 승인·요청 화면이 사람을 표시 이름으로 렌더링하므로 모든 계정이 하나를 갖는다(헤더는 표시 이름을 보이고 없으면 email fallback).
- 공개 회원가입은 제공하지 않음. admin이 사용자를 생성하면 24시간 유효한 일회용 password setup link를 발급하며 MVP에서는 이메일 발송 없이 한 번만 표시.
- **Google 소셜 로그인(OIDC):** Authorization Code + PKCE, `state`(CSRF)·`nonce`(replay) 필수, ID token 검증(서명·iss·aud·exp)과 `email_verified` 확인.
  흐름은 **서버 사이드 콜백**(`/auth/google/start`·`/auth/google/callback`)이며 프론트는 Google SDK를 쓰지 않고 백엔드 링크만 둔다.
  계정은 `(issuer, subject)`로 링크하고 verified email이 **admin이 만든 기존 사용자**와 일치할 때만 연결한다(자동 가입 없음). pending state(state/nonce/PKCE verifier)는 단명 AEAD 암호화 `__Host-` 쿠키로 전달.
  (ADR-0007)
- 마지막 active admin은 비활성화·삭제·강등할 수 없음.
  사용자 비활성화와 role 변경은 audit event를 남기고 기존 session을 즉시 revoke.
- 비활성화된 requester는 실행할 수 없고, 비활성화되거나 approver 권한을 잃은 사용자의 아직 실행되지 않은 승인은 실행 시 무효 처리.

### 8.4 Audit, 보존, 개인정보
- audit event는 애플리케이션 runtime 권한으로 수정·삭제할 수 없으며, 모든 관리자 설정 변경도 audit 대상.
- MVP는 audit event 삭제 API를 제공하지 않고 기본 무기한 보존. self-host 운영자가 메타데이터 DB backup·보존 책임을 가짐.
- connection archive는 실행 중 요청이 없을 때만 허용하며 credential 폐기·pool 종료·미실행 요청 만료를 한 작업으로 처리한다.
  연결된 과거 요청·실행·audit 내역은 삭제하지 않고, 내역 정리는 archive와 분리된 별도 retention 작업으로만 수행한다(4.3).
- **원문 SQL 보호:** bind parameter가 아닌 inline literal(예: `UPDATE users SET token='secret' WHERE email='x'`)이나 **comment**(예: `-- customer token: secret`)도 민감할 수 있으므로, access request·saved query version(파라미터 default value 포함)·migration artifact의 **원문 SQL은 parameter value와 함께 AEAD로 암호화** 저장한다.
  복호화는 승인 UI에서 권한 있는 사용자에게만 허용한다.
- **redactor 계약(audit·AI 전송 공유):** audit에 남기는 redacted SQL은 ① **SQL comment 제거** ② inline literal → typed placeholder ③ bind placeholder 유지로 만든다. **parsing/redaction 실패 시 fail-closed** — 원문을 절대 기록하지 않고 `payload_digest`와 statement type만 남긴다.
  AI Review(4.8) 전송 경로도 같은 redactor를 사용한다.
  `payload_digest`는 암호화·redaction 이전에, **§4.3의 승인 단위 전체(payload version·org·requester·connection·connection config version·policy version·statement class·normalized SQL·typed parameter values)의 canonical 직렬화**를 기준으로 계산한다(2026-07-24 정정, ADR-0018 — 이전 "정확한 SQL 기준" 서술은 §4.3와 상충했음; digest는 SQL만이 아니라 승인된 전부를 결합해 파라미터·connection·정책 변경도 새 요청을 요구한다).
  제출 시 민감한 literal이 감지되면 parameter 사용을 권고한다.
- request의 암호화된 SQL·parameter payload 기본 보존 기간은 90일.
  만료 후 ciphertext를 삭제해도 payload digest와 실행 metadata는 유지.
- result snapshot은 PostgreSQL UNLOGGED result store에 result별 DEK로 AES-256-GCM 암호화하고 기본 15분 TTL 후 삭제. audit 및 애플리케이션 로그에 결과 row를 복제하지 않음. crash/failover 유실은 허용하지만 대상 DB query를 자동 재실행하지 않음.

### 8.5 운영
- production TLS는 reverse proxy/ingress에서 종료.
  신뢰할 proxy 목록 밖의 forwarded header는 무시.
- `/livez`와 metadata DB 의존성을 확인하는 `/readyz`를 분리하고 graceful shutdown 시 새 실행을 차단.
- 구조화 로그와 metrics에는 request ID, 상태, duration, count만 포함하고 SQL·파라미터·credential은 기본 제외.
- schema migration 전 backup과 복구 절차를 문서화하고 지원 버전 간 upgrade test를 제공.
- 외부 telemetry는 기본 비활성화하며 사용자 승인 없이 query 또는 usage metadata를 전송하지 않음.

---

## 9. 배포

| 채널 | 내용 |
|---|---|
| Docker Compose (**MVP**) | server + PostgreSQL. local quickstart와 mounted secret 기반 production 예제 분리. |
| Helm Chart (**post-MVP**) | 클러스터 내 PostgreSQL(CNPG 옵션) + 외부 PG. `existingSecret`, ServiceAccount+최소 RBAC, probe, PodSecurityContext. |
| Terraform / OpenTofu Provider (**API 안정화 후**) | 제품 *안의 리소스*(connection/policy 등)를 CRUD. `terraform-plugin-framework` + Connect unary(HTTP) 클라이언트, 필요 시 REST gateway 경유. 양 레지스트리 등록. |

**단일 진실 공급원은 컨테이너 이미지.** compose `.env` 키와 Helm `values.yaml` 키를 동일하게 맞춰 문서/지원 부담을 줄인다. provider는 API가 안정된 뒤에 만든다(먼저 만들면 계속 깨짐).

**API 명세는 protobuf 단일 소스**다.
`proto/`에서 Connect Go 핸들러 인터페이스와 SolidJS용 TS 클라이언트를 함께 생성해 end-to-end 타입을 맞춘다.
Terraform provider는 REST/OpenAPI를 전제하므로, provider 착수 시 Connect unary를 그대로 쓰거나 protobuf에서 OpenAPI/REST gateway를 생성해 provider 클라이언트를 만든다(provider가 "API 안정화 후"라 이 전환 비용은 수용 가능).

---

## 10. 차별점 요약

| 비교 대상 | Portcullis의 차별점 |
|---|---|
| kviklet 0.9.2 | 쿼리 version·공유·파라미터·즐겨찾기→새 승인 요청 재사용(Core 2), schema 거버넌스와 통합 audit. UX 우위는 동일 과업으로 검증; 결과 저장·페이지네이션·credential 암호화는 기준선 |
| Bytebase | 진짜 무료 OSS 셀프호스트(HA 제한·기능 게이팅 없음), 가벼움(Core 1/2 단일 바이너리), 좁고 깊은 UX |
| Atlas Cloud | access governance까지 포함, 외부 SaaS 종속 없는 셀프호스트, 통합 audit |

**해자는 "기능 수"가 아니라 "OSS 셀프호스트 + 통합 + UX".** 기능 경쟁이 아닌 포지셔닝 싸움.

---

## 11. 로드맵 (개략)

```
0  토대         프로젝트 골격 + 인증 + 핵심 스키마 + secret/audit/session 기반
1  Core 1-PG    PostgreSQL connection → request → approve → execute → audit 수직 구현
2  Bridge       PostgreSQL/MySQL parity → 결정론 SQL 검토·기본 Read EXPLAIN
3  Core 2       두 DB 쿼리 저장·재사용 + schema status/dry-run/영향 미리보기 (apply 제외)
   ── MVP ──
4  access 확장  민감정보 마스킹 우선 → 임시 접근·다단계 승인·OIDC/LDAP
5  schema       M3 미리보기 계약 기반 schema 승인·apply·복구·verify 완성
6  배포          Helm(CNPG) 정비, API 안정화 후 Terraform/OpenTofu provider; M5·마스킹 이후 에이전트 등록·권한 → MCP Gateway (WebMCP 선택 후속)
7  later        BI 분석·공유(차트·대시보드), declarative GitOps, CNPG 자동발견, SIEM, ML/AI Review(4.8)
```

**출하 경계:** 단계 1(Core 1-PG, PostgreSQL 단독 거버넌스 루프)을 **first releasable alpha 경계**로 둔다.
문서에서 말하는 MVP는 단계 3 완료 시점이며, MySQL parity·SQL 검토·EXPLAIN(2)와 schema 미리보기를 포함한 Core 2(3)를 포함하며 MCP Gateway/WebMCP는 제외한다.
인터뷰 결과(1.4)에 따라 Core 2 범위를 조정할 여지는 남긴다.

개발 원칙: 토대 이후로는 **기능 단위 수직 개발**(서버 API + SolidJS 화면을 함께).
UX가 차별점이므로 API와 화면을 동시에 맞춘다.

**로드맵 관리:** 새로운 방향은 먼저 Later 후보로 기록하고, 사용자 수요·목표·선행 조건이 확인되면 구체적인 마일스톤으로 옮긴다.
착수 전에 PRD의 범위·인수 조건과 필요한 ADR을 갱신하며, 후보 추가만으로 구현 일정이나 지원을 확정하지 않는다.

---

## 12. 결정 로그 & 미해결

### 12.1 확정된 결정

- **승인 정책:** connection·statement 종류별 `required_approvals`(read/write/ddl 각 0~N, 기본 1).
  N명은 서로 다른 활성 approver이며 자기 승인은 금지.
  `N=0`은 system 자동 승인. request는 policy version을 snapshot하고 정책 변경 시 미실행 요청을 만료시킴(4.3).
- **Connection archive:** hard delete하지 않음.
  실행 중에는 archive를 거부하고 credential 폐기·pool 종료·미실행 요청 만료를 수행하며 과거 내역은 보존(4.3, 8.4).
- **Result store:** PostgreSQL `result_cache` schema의 UNLOGGED table + result별 AES-256-GCM DEK. application replica는 같은 primary에서 공유하고 crash/standby failover 유실은 허용.
  정렬·필터는 동시성이 제한된 server worker에서 일시 복호화해 처리(6, 7.1).
- **출하 경계:** Core 1-PG 완료를 first releasable alpha, MySQL과 Core 2까지 완료한 시점을 MVP로 정의(11).
- **임시 접근 = 웹 SQL console session:** 서버 실행 경로 재사용 → dialect 무관·자동 정책·결과 그리드·statement 단위 audit(6.1).
  DB proxy credential은 Later 보류(native client 수요 검증 시에만).
  UI는 터미널처럼 렌더하되 **기본 stateless per-statement**(idle-in-transaction 차단), 멀티 statement 트랜잭션은 connection 정책 opt-in + 하드 idle timeout·자동 ROLLBACK·read-only로 제한된 후속 옵션(4.6).

### 12.2 결정 상태 (2026-07-04 기준 — 미결정은 라이선스 1건뿐)

해소된 항목(각 ADR이 구속력 있는 명세):

| 항목 | 해소 |
|---|---|
| DB 최소 버전·driver/parser 라이브러리 | **ADR-0001** (pgx / go-sql-driver; PG=pgplex/pgparser, MySQL=tidb pkg/parser; SQLite는 ADR-0025로 제외; ADR-0030으로 최소 버전 범위를 대체: PG 16/17/18/19 호환성 유지(19는 GA 검증 전 preview); MySQL 8.4/9.7 LTS·26.7 Innovation 후보) |
| statement 분류표·edge fixture | **ADR-0002** (리터럴 fixture 27종, CTE-DML/`SELECT INTO` 구조 검출 확정) |
| master key 파일 형식·rotation·key 유실 정책 | **ADR-0003** (base64 단일 파일, `_PREVIOUS` 다중 버전 형식, eager batch rotation, 유실 시 복구 불가 명시; envelope/AAD/Argon2 전체 파라미터 포함) |
| metadata RLS | **ADR-0004** (MVP 미적용, RLS-ready 유지, cross-org 테스트 필수) |
| result 타입 계약 | **ADR-0005** (proto + 엔진별 scan-type→LogicalType 매핑표, NULL 정렬·tie-breaker 고정) |
| result store quota·축출/거부·autovacuum | **ADR-0011** (사용자 64MiB, 만료→본인LRU→전역LRU→거부; 수치는 Core 2 부하 테스트로 재확인하는 잠정치) |
| 임시 접근 위협 모델 | **§4.9** (세션 권한·만료·동시 실행·회수·transaction 경계) |
| Atlas pin·배포/NOTICE/checksum·compatibility matrix | **ADR-0012** (Community v1.2 라인, 정확한 patch는 M5 착수 시 pin) |
| migration artifact backend·크기·보존·`schema_apply_timeout`·lock 복구 | **ADR-0012** (metadata PG + AEAD, 파일 1MiB/artifact 10MiB/500파일, terminal+90일, 10분/상한 60분, lease row 복구) |
| 운영 기본값(타임아웃·요청 상한·rate limit·부팅 순서 등) | **ADR-0010** |
| cache 유실 runbook (Helm 전) | 모델은 §6·§12.1로 확정(재실행 없음·`result_unavailable`); Helm chart 작성 시 runbook **문서화 작업**만 남음(결정 아님) |

**유일한 미결정 — 공개(OSS 릴리스) 전, 소유자 결정:** Portcullis 자체 라이선스(Apache-2.0/AGPL 등)·CLA·상표·유료 기능 경계.
"진짜 무료 OSS" 포지셔닝(10)에 직결되며 문서·구현 작업으로 대신 정할 수 없는 사업 결정.
공개 전 반드시 확정.
(AI Review 4.8의 hosted 과금이 유력 후보)

---

## 13. 외부 전제 확인 기준

다음 공식 자료를 2026-06-27 기준으로 확인했다.
기능·라이선스 전제는 구현 착수와 릴리스 때 다시 검증한다.

- Kviklet은 **2026-09-30 재검증**: [최신 릴리스 API](https://api.github.com/repos/kviklet/kviklet/releases/latest), [0.9.2 보안 릴리스](https://github.com/kviklet/kviklet/releases/tag/0.9.2), [0.9.0 기능·proxy edition 변경](https://github.com/kviklet/kviklet/releases/tag/0.9.0), [0.8.0 UX·실행 권한 변경](https://github.com/kviklet/kviklet/releases/tag/0.8.0), [태그 고정 README](https://github.com/kviklet/kviklet/blob/0.9.2/Readme.md).
  세부 근거·영향은 ADR-0019.
- [Bytebase High Availability 문서](https://docs.bytebase.com/get-started/self-host/high-availability): self-host HA 요건과 HA-enabled license 요구.
- [Atlas Community Edition 문서](https://atlasgo.io/community-edition): Apache 2.0 Community 범위와 declarative plan, lint, pre-check, Go SDK 등 제외 기능.
- [PostgreSQL Unlogged Tables 문서](https://www.postgresql.org/about/featurematrix/detail/unlogged-tables/): crash 시 초기화되고 standby로 복제되지 않는 result cache 손실 모델.
