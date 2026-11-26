import { useEffect, useState } from "react"
import { Logo } from "@/components/logo"

const FACTS = [
  "It takes about 66 days on average for a new habit to feel automatic — not the famous 21.",
  "The word \"streak\" originally meant the lit fuse of a firework or cannon.",
  "Your brain releases dopamine on the anticipation of a reward, not just the reward itself.",
  "Seinfeld's productivity trick: never break the chain — mark every day you work, and keep it unbroken.",
  "The longest studied habit loop has three parts: cue, routine, reward.",
  "Habit stacking — attaching a new habit to an existing one — was named by behaviour researcher BJ Fogg.",
  "A 2009 study found people who flossed before brushing flossed more consistently.",
  "Writing down your goals makes you significantly more likely to achieve them.",
  "Checklists were introduced to surgery in 2009 and cut complications by over a third.",
  "Jerry Rice held his workout streak through every NFL off-season for 20 years.",
  "Your willpower is strongest early in the day — schedule hard habits first.",
  "The Pomodoro Technique is named after a tomato-shaped kitchen timer.",
  "Reviewing notes just before sleep improves recall — memory consolidates overnight.",
  "It takes muscles about 2,000 steps to walk a mile, roughly 15 minutes.",
  "The GIF format has been around since 1987 — older than the web itself.",
  "Honey never spoils. Archaeologists have eaten 3,000-year-old honey.",
  "A group of flamingos is called a flamboyance.",
  "Octopuses have three hearts and blue blood.",
  "Bananas are berries, but strawberries aren't.",
  "A day on Venus is longer than its year.",
  "The Eiffel Tower grows about 15 cm taller in summer heat.",
  "Wombat poop is cube-shaped.",
  "There are more possible chess games than atoms in the observable universe.",
  "The first computer bug was an actual moth, found in Harvard's Mark II in 1947.",
  "MySQL, Postgres and SQLite all turned 30 or older — and none of them have gone gray.",
  "The Go gopher's official name is Gordon.",
  "Greenland sharks can live for over 400 years.",
  "Your taste buds replace themselves every 10 days or so.",
]

// After a few seconds of waiting, share a random fact: the free API host sleeps
// when idle, so the first load can take up to a minute while it wakes.
export function SlowHint({ after = 4000 }: { after?: number }) {
  const [late, setLate] = useState(false)
  const [fact, setFact] = useState(() => Math.floor(Math.random() * FACTS.length))
  useEffect(() => {
    const t = setTimeout(() => setLate(true), after)
    return () => clearTimeout(t)
  }, [after])
  useEffect(() => {
    if (!late) return
    const t = setInterval(() => setFact((f) => (f + 1) % FACTS.length), 8000)
    return () => clearInterval(t)
  }, [late])
  if (!late) return null
  return (
    <p className="page-enter mx-auto max-w-sm text-center text-xs text-muted-foreground" role="status">
      Did you know? {FACTS[fact]}
    </p>
  )
}

// Full-screen wait: just the logo, beating.
export function LoadingScreen() {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 p-6" aria-busy role="status" aria-label="Loading">
      <div className="animate-breathe">
        <Logo className="text-lg" />
      </div>
      <SlowHint />
    </div>
  )
}
