# Portcullis

데이터베이스 접근과 변경을 통제하고, 승인된 쿼리의 결과를 탐색하는 셀프호스트 DevSecOps · BI 프로젝트입니다. Go 서버와 SolidJS 웹 UI를 하나의 바이너리로 배포합니다.

**현재는 PostgreSQL alpha 개발 단계입니다.** 아래 화면은 실제 개발 버전에서 합성 데이터로 캡처했습니다. M1의 전체 릴리스 검증은 아직 완료되지 않았으며, 네이티브 CSV 파일 저장 검증의 남은 문제는 [검증 기록](docs/operations/m1-validation.md)에 정리되어 있습니다.

## 요청하고, 검토하고, 한 번 실행하기

연결을 선택하고 SQL을 제출하면 요청의 내용과 적용 정책이 고정됩니다. 별도 승인자가 검토한 뒤 원래 요청자가 한 번 실행하며, 이미 실행된 요청은 다시 실행할 수 없습니다.

![요청자가 SQL을 제출하고 별도 승인자가 검토한 뒤 요청자가 한 번 실행하는 실제 사용 흐름](docs/media/workflow.gif)

GIF의 Alex는 요청자, Sam은 별도 승인자입니다. 데모용 승인자 계정은 캡처 환경의 DB fixture로 사전 구성했습니다. 사용자 관리 UI는 후속 마일스톤에서 제공할 예정입니다.

## 결과 탐색

결과 표에서 페이지를 넘기고, 숫자 열을 정렬하고, 원하는 값을 필터링할 수 있습니다. 큰 정수와 소수는 정밀도를 유지하며, 열의 논리 타입과 DB 타입을 함께 보여줍니다.

![합성 주문 데이터의 큰 정수, 지역, 소수 금액을 보여주는 실제 결과 화면](docs/media/results.png)

![페이지 탐색, 금액 내림차순 정렬, 지역 필터링, 전체 스냅샷 CSV 준비 과정](docs/media/results.gif)

CSV는 현재 페이지나 필터와 관계없이 캐시된 전체 결과를 대상으로 준비합니다. 이 GIF는 CSV 준비와 다운로드 링크 표시까지 보여주며, 운영체제에 실제 파일을 저장하는 성공 시연은 포함하지 않습니다. 결과 캐시는 15분 뒤 만료되며 용량 제한에 따라 더 일찍 제거될 수 있습니다.

## 주요 기능

| 기능 | 현재 개발 버전에서 제공하는 동작 |
| --- | --- |
| DB 연결 관리 | PostgreSQL 연결 등록·테스트, 개발/운영 환경 표시, 연결 보관 처리 |
| 연결별 실행 정책 | Read / Write / DDL 허용 여부, 종류별 승인 정족수, 시간·행·바이트 제한 |
| SQL 요청과 승인 | 편집 가능한 초안, 제출 내용 고정, 자기 승인 금지, 승인·반려·취소 |
| 통제된 실행 | 승인 및 정책 재검증, 승인당 단일 실행, 취소 요청, 불확실한 실행 결과의 명시적 표시 |
| 결과 탐색 | 암호화된 임시 스냅샷, 페이지 탐색·정렬·필터·전체 셀 보기·CSV 준비 |
| 감사와 접근 제어 | 서버 측 권한 검사, 상태 전이와 실행 증적을 기록하는 append-only 감사 이벤트 |
| 인증 | 최초 관리자 bootstrap, 이메일/비밀번호 로그인, 설정 시 Google OIDC 로그인 |

<details>
<summary>연결 관리 화면 보기</summary>

두 연결은 모두 격리된 데모 DB를 사용합니다. `production`은 환경 표시를 보여주기 위한 예시이며 실제 운영 DB가 아닙니다.

![PostgreSQL 연결 목록과 개발 및 운영 환경 표시](docs/media/connections.png)

</details>

<details>
<summary>연결별 정책 화면 보기</summary>

기본 정책은 Read만 허용하고 별도 승인자 한 명을 요구합니다. Write와 DDL은 관리자가 명시적으로 허용해야 합니다.

![Read, Write, DDL 허용 여부와 승인 수 및 실행 제한을 설정하는 정책 화면](docs/media/policy.png)

</details>

<details>
<summary>승인자의 요청 검토 화면 보기</summary>

승인자는 제출된 SQL과 대상 연결, 승인 현황을 확인하고 승인하거나 사유와 함께 반려합니다.

![별도 승인자가 SQL과 연결 스냅샷을 검토하고 승인 또는 반려하는 화면](docs/media/review.png)

</details>

## 로컬에서 시작하기

Docker Compose를 사용할 수 있는 환경에서 프로젝트 루트에서 실행합니다.

```sh
docker compose up --build
```

`http://localhost:8080`을 열어 최초 관리자 계정을 생성하고 로그인합니다. PostgreSQL 연결을 등록한 뒤 정책을 확인하고 SQL 요청을 제출합니다. 기본 승인 정책을 사용하려면 별도 승인자 계정을 사전 구성해야 합니다. 혼자 시험하는 격리된 데모에서는 Read approvals를 `0`으로 설정할 수 있으며, 이 경우에도 자동 승인 이력을 기록합니다.

자세한 연결 설정과 첫 실행 과정은 [PostgreSQL alpha 사용 안내](docs/operations/pg-alpha-quickstart.md)를 참고하세요. 예제의 DB 소유자 계정과 비활성화된 TLS는 일회성 로컬 시연을 위한 설정입니다.

## 개발과 로드맵

DDD, 소비자 정의 port를 사용하는 Clean Architecture, 시나리오 중심의 TDD를 개발 기준으로 삼습니다. 변경은 작은 단위로 커밋하며 매 커밋 직전에 staged diff를 리뷰합니다. 전체 검증 명령은 `make verify`입니다.

저장 쿼리·즐겨찾기·공유, MySQL·SQLite 실행, 차트·대시보드, 스키마 변경 거버넌스는 후속 작업입니다. 현재 제공되는 기능과 구분하여 [제품 요구사항](docs/product/prd.ko.md)에 범위와 인수 조건을 관리합니다.

[문서 안내](docs/README.md) · [Architecture](docs/ARCHITECTURE.md) · [ADRs](docs/adr/README.md) · [화면/GIF 갱신 방법](docs/media/README.md)
