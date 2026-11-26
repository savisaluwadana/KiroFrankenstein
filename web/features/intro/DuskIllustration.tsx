/**
 * Self-contained procedural dusk illustration for the intro: flat sky bands, a
 * skyline and a lit warehouse silhouette. Decorative only (no simulation data,
 * no numbers). The single moving van uses CSS and stops under reduced motion.
 */
const SKY = ["#2a2438", "#33283a", "#42303a", "#583a3a", "#764a3a", "#a56a3c", "#e8a33d"];

export function DuskIllustration() {
  const bandH = 70;
  return (
    <svg
      aria-hidden
      focusable="false"
      className="intro-sky absolute inset-0 h-full w-full"
      viewBox="0 0 1200 700"
      preserveAspectRatio="xMidYMid slice"
    >
      {SKY.map((c, i) => (
        <rect key={c} x="0" y={i * bandH} width="1200" height={bandH + 1} fill={c} />
      ))}
      {/* distant skyline */}
      <g fill="#2b2530">
        <rect x="40" y="380" width="70" height="140" />
        <rect x="120" y="340" width="50" height="180" />
        <rect x="180" y="400" width="90" height="120" />
        <rect x="860" y="360" width="60" height="160" />
        <rect x="930" y="410" width="100" height="110" />
        <rect x="1040" y="350" width="70" height="170" />
        <rect x="1120" y="390" width="80" height="130" />
      </g>
      {/* warehouse */}
      <g>
        <rect x="300" y="360" width="520" height="170" fill="#1d1b21" />
        <polygon points="290,360 560,300 830,360" fill="#17161a" />
        <rect x="330" y="470" width="80" height="60" fill="#0f0e11" />
        <rect x="430" y="470" width="80" height="60" fill="#0f0e11" />
        <rect x="530" y="470" width="80" height="60" fill="#0f0e11" />
        {[0, 1, 2, 3, 4, 5, 6, 7].map((i) => (
          <rect key={i} x={335 + i * 60} y="390" width="28" height="22" fill={i % 3 === 1 ? "#6d5430" : "#e8a33d"} />
        ))}
        <rect x="660" y="420" width="130" height="110" fill="#2a292f" />
        <rect x="672" y="430" width="106" height="8" fill="#e8a33d" />
      </g>
      {/* ground and road */}
      <rect x="0" y="530" width="1200" height="170" fill="#17161a" />
      <rect x="0" y="600" width="1200" height="6" fill="#2a292f" />
      <g className="intro-drift" style={{ transformBox: "fill-box" }}>
        <rect x="-140" y="560" width="90" height="34" fill="#f3ede0" />
        <rect x="-50" y="568" width="34" height="26" fill="#e8a33d" />
        <circle cx="-110" cy="596" r="8" fill="#0f0e11" />
        <circle cx="-30" cy="596" r="8" fill="#0f0e11" />
      </g>
    </svg>
  );
}
