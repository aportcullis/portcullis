import { expect, test, type Page } from "@playwright/test";

type CSPViolation = { directive: string; uri: string };

/** Starts a foreign resource load in the page and resolves with the CSP violation it reports. */
async function captureForeignViolation(page: Page, kind: "script" | "image" | "fetch" | "font"): Promise<CSPViolation> {
  return page.evaluate(requested => new Promise<CSPViolation>(resolve => {
    // A distinct host per probe keeps a late report from one probe out of another probe's result.
    const foreignHost = `${requested}.portcullis-csp.invalid`;
    const foreignURL = `https://${foreignHost}/probe`;
    document.addEventListener("securitypolicyviolation", event => {
      if (event.blockedURI.includes(foreignHost)) {
        resolve({ directive: event.effectiveDirective, uri: event.blockedURI });
      }
    });
    if (requested === "script") {
      const script = document.createElement("script");
      script.src = foreignURL;
      document.head.append(script);
    } else if (requested === "image") {
      const image = document.createElement("img");
      image.src = foreignURL;
      document.body.append(image);
    } else if (requested === "fetch") {
      fetch(foreignURL).catch(() => undefined);
    } else {
      new FontFace("ForeignProbe", `url(${foreignURL})`).load().catch(() => undefined);
    }
  }), kind);
}

test("embedded SPA works while inline and foreign scripts are refused", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: /^(Create admin account|Sign in)$/ })).toBeVisible();
  const inlineExecuted = await page.evaluate(() => {
    const script = document.createElement("script");
    script.textContent = "document.documentElement.dataset.cspInlineProbe = 'executed'";
    document.head.append(script);
    return document.documentElement.dataset.cspInlineProbe === "executed";
  });
  expect(inlineExecuted).toBe(false);

  const violation = await captureForeignViolation(page, "script");
  expect(violation.directive).toMatch(/^script-src(?:-elem)?$/);
  expect(violation.uri).toContain("portcullis-csp.invalid");
});

test("same-origin images, data images, inline styles and RPCs keep working", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: /^(Create admin account|Sign in)$/ })).toBeVisible();
  const loaded = await page.evaluate(async () => {
    const loadImage = (source: string) => new Promise<boolean>(resolve => {
      const image = new Image();
      image.onload = () => resolve(true);
      image.onerror = () => resolve(false);
      image.src = source;
    });
    const probe = document.createElement("div");
    probe.setAttribute("style", "width: 17px");
    document.body.append(probe);
    const styled = probe.getBoundingClientRect().width === 17;
    const response = await fetch("/livez");
    return {
      sameOriginImage: await loadImage("/brand/icon.svg"),
      dataImage: await loadImage("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='1' height='1'/%3E"),
      styled,
      sameOriginFetch: response.ok,
    };
  });
  expect(loaded).toEqual({ sameOriginImage: true, dataImage: true, styled: true, sameOriginFetch: true });
});

test("foreign images, fetches and fonts are refused by their own directives", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: /^(Create admin account|Sign in)$/ })).toBeVisible();
  const image = await captureForeignViolation(page, "image");
  expect(image.directive).toBe("img-src");
  const fetched = await captureForeignViolation(page, "fetch");
  expect(fetched.directive).toBe("connect-src");
  const font = await captureForeignViolation(page, "font");
  expect(font.directive).toBe("font-src");
});
