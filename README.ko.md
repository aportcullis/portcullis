# Portcullis

한국어 · [English](README.md)

![Portcullis 로고](docs/media/logo.png)

**셀프호스트 데이터베이스 거버넌스. SQL을 요청하고, 검토하고, 한 번 실행한 뒤 결과를 확인합니다.**

[![라이선스 Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![CI](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml/badge.svg)](https://github.com/aportcullis/portcullis/actions/workflows/ci.yml)

[빠른 시작](#시작하기) · [화면 둘러보기](#화면-둘러보기) · [문서](#문서) · [변경 내역](CHANGELOG.md) · [커뮤니티](#커뮤니티) · [기여](#기여)

Portcullis는 자체 인프라에서 SQL 요청·승인·실행·감사를 관리하는 도구입니다. 실행 전에 SQL과 파라미터를 검토하고, 같은 화면에서 결과를 확인할 수 있습니다. 웹 UI는 Go 바이너리에 포함되며 메타데이터는 PostgreSQL에 저장합니다.

**개발 중인 alpha:** PostgreSQL의 요청·승인·실행 흐름은 테스트를 통과했습니다. 사용자·역할 관리와 릴리스 준비는 진행 중입니다. 검증된 범위는 [데이터베이스 지원 현황](docs/product/database-support.md)과 [검증 결과](docs/milestones/m1/validation.md)를 확인하세요. 전체 MVP는 후속 마일스톤에서 완성합니다.

## 주요 기능

- **승인 정책:** 연결별로 허용할 SQL 종류, 필요한 승인 수와 실행 한도를 설정합니다.
- **요청과 검토:** SQL에 설명과 파라미터를 추가해 제출합니다. 검토자는 제출된 내용을 그대로 확인합니다.
- **한 번 실행:** 실행 전에 승인과 정책을 다시 확인합니다. 결과가 불확실해도 SQL을 자동으로 재실행하지 않습니다.
- **결과 탐색:** 쿼리를 다시 실행하지 않고 정렬·필터·셀 확인·Table/Text 전환·페이지 복사·CSV 내보내기를 사용합니다.

<details>
<summary>보안과 셀프호스트 운영</summary>

서버에서 권한을 검사하고 자격증명·요청 데이터·캐시된 결과를 암호화합니다. 캐시된 결과는 만료되며 감사 기록은 추가만 가능합니다. 연결 정책으로 실행 시간·행 수·결과 크기를 제한합니다.

Docker Compose와 내장 웹 UI로 실행합니다. 로컬 로그인을 제공하며 Google OIDC는 선택 사항입니다. 자세한 내용은 [아키텍처](docs/ARCHITECTURE.md)와 [운영 안내](docs/operations/pg-alpha-quickstart.md)를 확인하세요.

</details>

## 시작하기

Git과 Docker를 설치하세요. Docker Compose도 필요합니다.

```sh
git clone https://github.com/aportcullis/portcullis.git
cd portcullis
docker compose up --build
```

1. [localhost:8080](http://localhost:8080)에 접속하세요.
2. `docker compose logs portcullis`에서 setup token을 복사해 최초 관리자를 생성하세요.
3. [PostgreSQL 빠른 시작](docs/operations/pg-alpha-quickstart.md)에 따라 연결을 등록하고 첫 요청을 제출하세요.

최초 관리자 이메일과 비밀번호를 미리 설정하면 시작할 때 계정을 생성합니다. setup token 없이 바로 로그인하면 됩니다. 설정 항목은 [.env.example](.env.example)에 있습니다.

운영 환경에서는 Portcullis를 내부 네트워크에 두고 Cloudflare WARP나 Tailscale로 접속하세요. 자세한 구성은 [권장 배포 방식](docs/operations/recommended-architecture.md)을 확인하세요.

<details>
<summary>로컬 데모 설정과 데이터 유지</summary>

기본 정책에서는 다른 사용자의 승인이 필요합니다. 혼자 쓰는 일회성 데모에서는 **Read approvals**를 `0`으로 설정하세요.

Compose는 메타데이터와 master key를 볼륨에 저장합니다. 예제 자격증명과 데이터베이스 TLS 비활성화는 로컬 데모에서만 사용하세요. 백업·운영 조건·실행 제한은 빠른 시작 문서에 있습니다.

</details>

## 화면 둘러보기

예제 데이터로 실제 애플리케이션을 촬영했습니다.

### 요청·검토·실행

SQL을 제출하고 다른 사용자의 승인을 받은 뒤 한 번 실행합니다.

![SQL 제출, 다른 사용자의 승인과 한 번 실행하는 흐름](docs/media/workflow.gif)

<details>
<summary>요청·검토 화면 보기</summary>

설명과 SQL, 파라미터를 입력해 요청을 작성합니다.

![요청 작성 화면](docs/media/request.png)

검토자는 제출된 SQL을 확인하고 승인하거나 거절합니다.

![승인·거절 화면](docs/media/review.png)

요청을 펼치면 진행 상태를 확인할 수 있습니다. 조회할 수 없는 이력과 불확실한 실행 결과도 표시합니다.

![작성부터 실행까지의 요청 진행 상태](docs/media/requests.png)

</details>

### 결과 탐색

캐시된 결과를 정렬·필터링하고, 페이지를 복사하거나 CSV로 내보냅니다.

![결과 페이지 이동, 정렬, 필터와 CSV 준비](docs/media/results.gif)

<details>
<summary>Table·Text 화면 보기</summary>

정렬은 캐시된 전체 결과에 적용되며 숫자 정밀도를 유지합니다. Table/Text와 클립보드 복사는 현재 필터된 페이지를 사용합니다. CSV는 원래 쿼리 순서로 전체 결과를 내보냅니다. GIF는 다운로드 링크에서 끝납니다.

![정렬과 복사 기능이 있는 결과 표](docs/media/results.png)

Text 화면에서는 긴 값을 줄바꿈합니다. 실행 요약에는 영향받은 행 수와 서버 처리 시간을 표시합니다. 처리 시간은 DB 연결·실행·결과 수집·저장을 포함합니다.

![Text 결과 화면](docs/media/results-text.png)

</details>

<details>
<summary>연결·정책 화면 보기</summary>

데이터베이스 연결을 등록하고 허용할 SQL 종류, 필요한 승인 수와 실행 한도를 설정합니다.

![데이터베이스 연결 목록](docs/media/connections.png)

![연결별 승인 정책과 실행 한도](docs/media/policy.png)

</details>

자세한 설명은 [제품 둘러보기](docs/media/product-tour.md)를 확인하세요.

## 문서

| 목적 | 시작 문서 |
| --- | --- |
| Portcullis 실행 | [PostgreSQL 빠른 시작](docs/operations/pg-alpha-quickstart.md) |
| 데이터베이스 지원 확인 | [기능별 지원 현황과 검증 범위](docs/product/database-support.md) |
| 시스템 이해 | [아키텍처](docs/ARCHITECTURE.md) · [운영 검증](docs/milestones/m1/validation.md) |
| 다음 기능 확인 | [마일스톤](docs/milestones/README.md) · [제품 요구사항](docs/product/prd.ko.md) |
| 개발과 UI 변경 | [개발 안내](docs/development.md) · [UI 변경 규칙](docs/conventions/frontend.md#changing-the-ui) · [컴포넌트 목록](web/src/shared/ui/README.md) |

전체 안내는 [문서 인덱스](docs/README.ko.md)를 확인하세요. 제품 요구사항과 안내 페이지는 한국어·영어로, 나머지 문서는 현재 영어로 제공합니다.

## 커뮤니티

버그·질문·피드백은 [GitHub 이슈](https://github.com/aportcullis/portcullis/issues)에 남겨주세요. 참여 방법은 [COMMUNITY.md](COMMUNITY.md)를 확인하세요.

보안 취약점은 [SECURITY.md](SECURITY.md)의 절차에 따라 비공개로 신고하세요.

## 기여

문서·버그 재현·테스트·작은 개선에 참여할 수 있습니다. [CONTRIBUTING.md](CONTRIBUTING.md)를 먼저 읽고, 큰 변경은 구현 전에 이슈에서 논의하세요.

## 라이선스

[Apache License 2.0](LICENSE)을 적용합니다. 저작권과 출처는 [NOTICE](NOTICE)를 확인하세요. 외부 컴포넌트에는 각 라이선스를 유지합니다.
