import { PasswordSetupForm } from "@/features/auth/PasswordSetupForm";
import { BrandLogo } from "@/shared/ui/BrandLogo";
import { Card, CardContent, CardHeader } from "@/shared/ui/card";

/** Assembles public initial-password registration independently of authenticated navigation. */
export default function PasswordSetupPage() {
  return <main class="flex min-h-screen items-center justify-center p-4"><Card class="w-full max-w-sm"><CardHeader><BrandLogo class="h-12" /><h1 class="text-xl font-semibold">Set your password</h1></CardHeader><CardContent><PasswordSetupForm /></CardContent></Card></main>;
}
