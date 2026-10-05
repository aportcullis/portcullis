# Portcullis

한국어 · [English](README.md)

![Portcullis 로고](docs/media/logo.png)

**셀프호스트 데이터베이스 거버넌스. 요청하고, 검토하고, 한 번 실행하고, 결과를 탐색합니다.**

[![라이선스 Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![CI](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml/badge.svg)](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml)

[빠른 시작](#시작하기) · [화면 둘러보기](#화면-둘러보기) · [문서](#문서) · [변경 내역](CHANGELOG.md) · [커뮤니티](#커뮤니티) · [기여](#기여)

Portcullis는 SQL 요청·승인·실행·감사를 하나의 애플리케이션에서 관리합니다. 실행할 SQL과 파라미터를 검토하고, 연결별 정책을 적용하며, 자체 인프라에서 승인된 실행의 결과를 탐색할 수 있습니다. Go 바이너리 하나에 웹 UI를 포함하고 메타데이터는 PostgreSQL에 저장합니다.

**개발 중인 alpha:** PostgreSQL 거버넌스 흐름에는 검증 기록이 있으며, M1 사용자·역할 관리와 릴리스 보호 장치는 진행 중입니다. 전체 MVP는 후속 마일스톤에서 완성합니다. 도입 전에 [데이터베이스 지원 현황](docs/product/database-support.md)과 [검증 기록](docs/milestones/m1/validation.md)을 확인하세요.

## 주요 기능

- **접근 관리:** 연결마다 Read / Write / DDL 권한, 승인 인원과 실행 한도를 설정합니다.
- **맥락이 있는 검토:** 제목·본문·SQL·타입이 있는 파라미터로 요청을 작성하고, 진행 상태와 제출 후 고정된 내용을 검토합니다.
- **한 번 실행:** 승인과 정책을 다시 확인한 뒤 실행하고, 결과가 불확실해도 SQL을 자동으로 재실행하지 않습니다.
- **결과 탐색:** 쿼리를 다시 실행하지 않고 열 정렬·필터·셀 확인·Table/Text 전환·현재 페이지 복사·CSV 내보내기를 사용합니다.

<details>
<summary>보안과 셀프호스트 운영</summary>

권한은 서버에서 검사합니다. 자격증명과 요청 내용은 암호화하고, 암호화된 결과 스냅샷은 만료되며, 감사 기록은 추가만 가능합니다. 연결 정책으로 실행 시간·행 수·결과 크기를 제한합니다.

Docker Compose와 내장 웹 UI로 실행합니다. 로컬 인증을 제공하며 Google OIDC는 선택 사항입니다. 시스템 경계와 운영 조건은 [아키텍처](docs/ARCHITECTURE.md)와 [운영 빠른 시작](docs/operations/pg-alpha-quickstart.md)을 확인하세요.

</details>

## 시작하기

Git과 Docker Compose를 포함한 Docker를 설치한 뒤 실행합니다.

```sh
git clone https://github.com/aportcullis/portcullis.git
cd portcullis
docker compose up --build
```

[localhost:8080](http://localhost:8080)에 접속하고 `docker compose logs portcullis`에 출력된 일회용 setup token으로 최초 관리자를 생성합니다. [PostgreSQL 빠른 시작](docs/operations/pg-alpha-quickstart.md)에 따라 대상 연결을 등록하고 첫 요청을 제출하세요.
최초 관리자 이메일과 비밀번호를 미리 설정하면 시작할 때 계정을 생성하고 토큰 발급과 초기 설정 화면을 건너뜁니다. 바로 로그인하면 됩니다. 설정 항목은 [.env.example](.env.example)을 확인하세요.

운영 환경에서는 [권장 사설 네트워크 구성](docs/operations/recommended-architecture.md)에 따라 Portcullis를 내부 네트워크에 두고 Cloudflare WARP나 Tailscale로 접속하세요.

<details>
<summary>로컬 데모 설정과 데이터 유지</summary>

기본 정책은 요청자와 다른 검토자 한 명의 승인을 요구합니다. 폐기 가능한 단일 사용자 데모에서는 **Read approvals**를 `0`으로 설정하세요.

Compose는 메타데이터와 master key를 볼륨에 보관합니다. 예제 자격증명과 데이터베이스 TLS 비활성화는 로컬 데모용입니다. 운영 조건·백업·실행 제한은 빠른 시작 문서를 확인하세요.

</details>

## 화면 둘러보기

SQL과 맥락을 제출하고 → 다른 사용자가 검토하고 → 한 번 실행한 뒤 → 결과를 탐색합니다. 아래 이미지는 가상 데이터로 실제 애플리케이션을 촬영한 것입니다.

### 요청과 검토

![요청자가 SQL을 제출하고 다른 검토자가 승인한 뒤 요청자가 한 번 실행하는 흐름](docs/media/workflow.gif)

요청 맥락과 SQL을 별도 영역에 배치합니다. 검토자는 제출된 SQL 옆에서 승인·거절을 선택합니다.

![맥락과 SQL을 나누고 검토 안내를 표시하는 요청 작성 화면](docs/media/request.png)

![SQL 옆에서 승인·거절을 선택하는 검토 화면](docs/media/review.png)

데모의 검토자 계정은 테스트 데이터로 생성했습니다. 사용자 관리 화면은 M1 범위입니다.

### 요청 진행 상태

요청 제목을 누르면 행 아래에서 Draft → Review → Ready → Execution 흐름을 확인할 수 있습니다. 조회할 수 없는 이력과 불확실한 실행 결과는 명시적으로 표시합니다.

![검토 대기 요청을 펼쳐 네 단계 진행 상태를 확인하는 화면](docs/media/requests.png)

### 결과 탐색

![결과 페이지 이동, 매출 정렬, 지역 필터와 전체 스냅샷 CSV 준비](docs/media/results.gif)

검색·정렬에는 레이블이 있고 복사·CSV 작업은 별도 영역에 모았습니다. 열 정렬은 캐시된 전체 스냅샷을 대상으로 하며 숫자 정밀도를 유지합니다. Table/Text와 클립보드 복사는 현재 필터된 페이지를 사용하고, CSV는 원래 쿼리 순서로 전체 스냅샷을 내보냅니다. GIF는 준비된 다운로드 링크에서 끝납니다.

![정확한 큰 정수와 소수 값, 페이지 이동과 복사를 제공하는 결과 표](docs/media/results.png)

Text 화면은 긴 값을 잘라내지 않고 줄바꿈합니다. 실행 요약에는 서버에서 측정한 시간과 영향받은 행 수를 표시합니다. 측정 시간에는 DB 연결·SQL 실행·결과 수집·스냅샷 저장을 포함합니다.

![같은 결과 페이지를 탭으로 구분해 보여주는 Text 화면](docs/media/results-text.png)

<details>
<summary>연결 등록과 정책 설정</summary>

요청을 제출하기 전에 대상 연결을 등록하고 허용할 SQL 종류·승인 조건·실행 한도를 설정합니다.

![대상 연결 등록과 기존 연결 목록](docs/media/connections.png)

![연결별 승인 조건과 실행 한도](docs/media/policy.png)

</details>

추가 화면과 흐름 설명은 [제품 둘러보기](docs/media/product-tour.md)를 확인하세요.

## 문서

| 목적 | 시작 문서 |
| --- | --- |
| Portcullis 실행 | [PostgreSQL 빠른 시작](docs/operations/pg-alpha-quickstart.md) |
| 데이터베이스 지원 확인 | [기능별 지원 현황과 검증 범위](docs/product/database-support.md) |
| 시스템 이해 | [아키텍처](docs/ARCHITECTURE.md) · [운영 검증](docs/milestones/m1/validation.md) |
| 다음 기능 확인 | [마일스톤](docs/milestones/README.md) · [제품 요구사항](docs/product/prd.ko.md) |
| 개발과 UI 변경 | [개발 안내](docs/development.md) · [UI 변경 규칙](docs/conventions/frontend.md#changing-the-ui) · [컴포넌트 목록](web/src/shared/ui/README.md) |

전체 문서는 [문서 인덱스](docs/README.ko.md)에서 확인하세요. PRD와 안내 페이지 외의 문서는 현재 영어로 제공합니다.

## 커뮤니티

애플리케이션을 사용하고 피드백을 나누거나 개선에 참여하세요. 참여 방법은 [COMMUNITY.md](COMMUNITY.md)에 있습니다. 버그·질문·피드백은 [GitHub 이슈](https://github.com/aportcullis/portcullis/issues)를 이용하세요. Discussions 운영은 아직 확인하지 않았습니다.

보안 취약점이 의심되면 공개 이슈 대신 [SECURITY.md](SECURITY.md)의 비공개 신고 절차를 따르세요.

## 기여

문서 개선·버그 재현·테스트·작은 개선을 환영합니다. [CONTRIBUTING.md](CONTRIBUTING.md)에서 시작하고, 큰 변경은 구현 전에 이슈에서 논의하세요.

## 라이선스

[Apache License 2.0](LICENSE)을 적용합니다. 저작권과 출처는 [NOTICE](NOTICE)를 확인하세요. 외부 컴포넌트에는 각 라이선스를 유지합니다.
