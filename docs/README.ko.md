# Portcullis 문서

한국어 · [English](README.md)

제품 요구사항은 한국어와 영어로 함께 관리합니다. 다른 문서도 번역을 추가할 때 아래 규칙을 적용합니다.

## 문서 목록

| 분야 | 문서 | 언어 |
| --- | --- | --- |
| 제품 요구사항 | [한국어 PRD](product/prd.ko.md) · [영어 PRD](product/prd.en.md) | 한국어 / 영어 |
| 데이터베이스 지원 | [기능별 지원 현황과 검증 범위](product/database-support.md) | 영어 |
| 데이터베이스 조사 | [컨테이너 기반 후보와 엔진 실험](product/database-candidates.md) | 영어 |
| 아키텍처 | [코드 계층과 쿼리 흐름](ARCHITECTURE.md) | 영어 |
| 컨테이너 릴리스 | [태그 기반 GHCR 배포](operations/container-releases.md) | 영어 |
| 배포 구성 | [WARP와 Tailscale을 이용한 사설 네트워크](operations/recommended-architecture.md) | 영어 |
| 기술 결정 | [ADR 인덱스](adr/README.md) | 영어 |
| 개발 규칙 | [코드](conventions/code.md) · [데이터](conventions/data.md) · [보안](conventions/security.md) · [도구](conventions/tooling.md) · [프론트엔드](conventions/frontend.md) | 영어 |
| 성능 | [시나리오와 용량](performance/README.md) · [쿼리 측정](performance/benchmarks/2026-09-30-query-workloads.md) | 영어 |
| 운영 | [PostgreSQL alpha 빠른 시작](operations/pg-alpha-quickstart.md) · [M1 검증](milestones/m1/validation.md) · [Docker 브라우저 E2E](operations/browser-e2e.md) · [암호화 키 교체](operations/key-rotation.md) | 영어 |
| UX 설계 | [연구 근거와 평가 절차](design/ux-evidence.md) · [UI 컴포넌트 목록과 재사용](../web/src/shared/ui/README.md) | 영어 |
| 브랜드 | [로고와 배치](branding.md) · [제품 둘러보기](media/product-tour.md) · [화면 이미지와 GIF](media/README.md) | 영어 |
| 로드맵 | [마일스톤과 제품 방향](milestones/README.md) | 영어 |
| 커뮤니티 | [참여 방법](../COMMUNITY.md) · [공개 준비와 첫 이슈 초안](community/launch-checklist.md) | 영어 |
| 기여 | [기여 안내](../CONTRIBUTING.md) · [개발 안내](development.md) | 영어 |

## 언어 정책

- **하나의 요구사항을 두 언어로 관리합니다.**
  - 번역 문서는 같은 주제 폴더에 `<name>.ko.md`와 `<name>.en.md`로 둡니다.
  - 루트와 디렉터리 안내 페이지는 영어 진입점인 `README.md`를 유지하고 한국어는 `README.ko.md`로 둡니다.
  - 문서 본문은 한 언어로 작성하고 상단에 다른 언어로 이동하는 링크를 둡니다.
  - 절 번호·요구사항 범위·상태·식별자·제한값·근거 링크를 두 번역에서 동일하게 유지합니다.
  - 두 파일을 같은 변경에서 갱신합니다. 번역은 요약이 아니라 전체 문서입니다.
- **버전은 Git 태그로만 부여합니다.**
  - 문서에 별도 개정 번호나 상태 번호를 두지 않습니다. 변경은 Git 이력으로 관리합니다.
- **의미 차이를 명시적으로 해결합니다.**
  - 초기 번역은 기존 한국어 PRD를 기준으로 합니다.
  - 의미가 다르면 구현 전에 한국어 요구사항을 확정하고 영어도 갱신합니다. 기술 결정은 ADR로 기록합니다.
- **번역은 점진적으로 추가합니다.**
  - PRD와 루트·문서 안내 페이지는 한국어·영어를 제공합니다. 아키텍처·ADR·개발 규칙·성능 문서는 현재 영어입니다.
  - 번역 쌍을 인덱스에 등록하고 서로 연결합니다.
  - ADR 번호와 기존 파일명을 유지해 기술 결정 참조가 바뀌지 않도록 합니다.
- **문장을 관리하기 쉽게 작성합니다.**
  - 관련 문장은 같은 문단에 두고, 주제가 바뀌는 문장이 끝나면 줄을 나눕니다.
  - 일반적인 개발 용어를 사용하고 기술 식별자와 검증 근거의 버전을 유지합니다.
  - 규칙은 짧은 상위 항목으로, 조건·예시·예외는 하위 항목으로 작성합니다.
  - 행 번호보다 절을 참조하고, 문서를 옮길 때 상대 링크를 갱신합니다.

## 파일 구조

```text
docs/
  README.md
  README.ko.md
  product/
    prd.ko.md
    prd.en.md
  ARCHITECTURE.md
  adr/
  conventions/
  milestones/
    README.md
    overview.svg
    m0/scope.md
    m1/scope.md
    m1/progress.md
    m1/validation.md
    m2/ … m7/
  performance/
    README.md
    benchmarks/
```

PRD는 저장소에서 관리합니다. 실행 작업 목록·리뷰 지적 사항·후속 메모는 로컬에서만 관리하며 커밋된 문서에서 참조하지 않습니다.

Per [ADR-0054](adr/0054-release-branches-and-documentation-policy.md), 확정된 결정은 새 ADR로 대체하며 PRD 두 번역의 요구사항과 절 번호는 함께 유지합니다.
