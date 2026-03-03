import s from './Footer.module.css'

export default function Footer() {
  return (
    <footer className={s.footer}>
      <div className={s.left}>
        <span className={s.brand}>LAN SUITE</span>
        <span className={s.version}>v0.1.0</span>
      </div>

      <div className={s.center}>
        <span>CONTENT-CENTRIC NETWORKING</span>
        <span className={s.sep}>·</span>
        <span>ED25519 IDENTITY</span>
        <span className={s.sep}>·</span>
        <span>ZERO CLOUD</span>
      </div>

      <div className={s.right}>
        <a
          href="https://h-strangeone.github.io/portfolio2/"
          className={s.link}
          data-mag="0.3"
        >
          CONTACT
        </a>
        <a
          href="https://github.com/H-strangeone/lan-suite"
          target="_blank"
          rel="noreferrer"
          className={s.link}
          data-mag="0.3"
        >
          GITHUB
        </a>
        <span className={s.credit}>
          BUILT BY <span className={s.author}>H-STRANGEONE</span>
        </span>
      </div>
    </footer>
  )
}