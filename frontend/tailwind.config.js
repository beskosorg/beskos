/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {},
    // The project's breakpoints. `md` = 721px so that `max-md:` matches EXACTLY the mobile
    // breakpoint webmail.css already uses (max-width:720px) — no dead zone between the two systems.
    screens: {
      sm: "640px",
      md: "721px",
      lg: "1024px",
      xl: "1280px",
    },
  },
  // The whole app lives under `.wm` (high-specificity CSS). `important: true` makes Tailwind's
  // utilities win that fight. It's safe because the app barely used utility classes before — so it
  // doesn't change the existing layout; it only enables the responsive utilities (max-md:*) we added.
  important: true,
  plugins: [],
};
