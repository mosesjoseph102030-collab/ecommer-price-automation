import SiteHeader from "@/components/SiteHeader";
import SiteFooter from "@/components/SiteFooter";
import Hero from "@/components/Hero";
import ProofStrip from "@/components/ProofStrip";
import ProblemOutcome from "@/components/ProblemOutcome";
import Workflow from "@/components/Workflow";
import Features from "@/components/Features";
import UseCases from "@/components/UseCases";
import Pricing from "@/components/Pricing";
import Faq from "@/components/Faq";
import FinalCta from "@/components/FinalCta";

// The section order below is the homepage wireframe in file.md, in order:
// header, hero, proof strip, problem/outcome, workflow, features, use cases,
// pricing, FAQ, final CTA, footer. The hero is the only section that carries the
// background image; everything below it is a plain surface so the page returns to
// readable body text as the spec requires.
export default function Home() {
  return (
    <>
      <SiteHeader />
      <main>
        <Hero />
        <ProofStrip />
        <ProblemOutcome />
        <Workflow />
        <Features />
        <UseCases />
        <Pricing />
        <Faq />
        <FinalCta />
      </main>
      <SiteFooter />
    </>
  );
}
