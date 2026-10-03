# Portcullis branding

The logo combines a gate with three teal bars and two abstract crow wings. The illustrated crow mascot is a separate branding asset; it does not replace the application's logo or favicon.

## Assets and placement

- [Wordmark](../web/public/brand/logo.svg): README, application header, login and bootstrap screens.
- [Dark wordmark](../web/public/brand/logo-dark.svg): light ink for the README's dark background.
- [Icon](../web/public/brand/icon.svg): browser favicon. Its warm background keeps the navy silhouette visible in both light and dark browser chrome.
- `shared/ui/BrandLogo.tsx`: the common application image with an accessible name and intrinsic dimensions.

These are native SVG interpretations of the approved direction, rather than embedded raster previews. The `frame`, `gate`, `wings` and `wordmark` groups remain individually editable. The wordmark uses a system font stack, so letter shapes vary slightly across platforms. All SVGs are self-contained and contain no scripts, remote resources or external font dependencies. When changing the symbol, update the matching groups in all variants.

Use midnight navy `#102A36`, teal `#147D82` and warm off-white `#F5F4EF`. Keep the logo at its original aspect ratio. Use the wordmark where there is room to read the name and the icon in square or very small slots. Avoid stretching, glow, gradients and repeating the mark inside each table or dialog.

The application loads `/brand/logo.svg` and `/brand/icon.svg` from Vite's public directory. Vite copies them into the embedded SPA build, following its [static asset documentation](https://vite.dev/guide/assets.html#the-public-directory). The README uses the repository-relative source path so the same artwork appears on GitHub.
