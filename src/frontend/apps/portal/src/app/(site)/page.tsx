import Link from "next/link"

import { Button, Card, CardDescription, CardHeader, CardTitle } from "@evdr/ui"
import { LockKeyhole, ScrollText, Stamp } from "lucide-react"

const PILLARS = [
  {
    icon: LockKeyhole,
    title: "Secure by design",
    body: "Access is enforced server-side and every page view is audit-logged — your documents never leave your infrastructure.",
  },
  {
    icon: ScrollText,
    title: "Full audit trail",
    body: "SIEM-grade audit events for every action: who viewed what, when, and from where.",
  },
  {
    icon: Stamp,
    title: "Branded rooms",
    body: "Each room carries the host's logo, colours and metadata — a counterparty-facing surface, not a shared inbox.",
  },
]

export default function HomePage() {
  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-16 sm:px-6">
      <section className="mx-auto max-w-3xl text-center">
        <h1 className="text-4xl font-semibold tracking-tight sm:text-5xl">
          Secure document exchange, built for regulated enterprises
        </h1>
        <p className="mt-4 text-lg text-muted-foreground">
          EVDR is a compliance-grade virtual data room. Review documents in branded,
          audit-tracked rooms — with dynamic watermarks and view-only enforcement.
        </p>
        <div className="mt-8 flex flex-wrap justify-center gap-3">
          <Button asChild size="lg">
            <Link href="/rooms">Browse data rooms</Link>
          </Button>
          <Button asChild variant="outline" size="lg">
            <Link href="/rooms">I have an invite</Link>
          </Button>
        </div>
      </section>

      <section className="mt-16 grid gap-4 sm:grid-cols-3">
        {PILLARS.map(({ icon: Icon, title, body }) => (
          <Card key={title}>
            <CardHeader>
              <Icon className="size-5 text-primary" aria-hidden />
              <CardTitle className="mt-3">{title}</CardTitle>
              <CardDescription>{body}</CardDescription>
            </CardHeader>
          </Card>
        ))}
      </section>
    </div>
  )
}
