# Avatar

Stable, locally generated five-by-five profile image. Use an opaque user ID as `seed` and an accessible description as `label`; keep the user's name visible alongside it. No network, persistent state or image service is required.

```tsx
import { Avatar } from "@/shared/ui/avatar";

<Avatar seed={user.id} label={`Profile image for ${user.displayName}`} />
```

Use `class` to change size or rounding. The background uses the theme's muted color and the pattern keeps its user-specific color in both themes. The image is a decorative identity cue, not proof of identity or guaranteed unique. It does not support uploads or remote URLs.

Portcullis-owned source: Apache-2.0. See [ADR-0041](../../../../../docs/adr/0041-default-profile-identicons.md).
