import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, SiteConfig } from '../api'

function CodeBlock({ value, placeholder }: { value?: string; placeholder: string }) {
  return (
    <div className={`rounded border border-washi-border px-4 py-3 font-mono text-sm break-all select-all ${value ? 'text-washi-muted' : 'text-washi-muted italic'}`}>
      {value || placeholder}
    </div>
  )
}

export default function HowToPage() {
  const [config, setConfig] = useState<SiteConfig | null>(null)
  useEffect(() => { api.config().then(setConfig).catch(() => {}) }, [])
  const genrePrefix = config?.genrePrefix

  return (
    <div className="max-w-2xl space-y-10 text-base text-washi-text">
      <h1 className="text-xl font-black text-washi-text">使い方</h1>

      <section className="space-y-4">
        <h2 className="text-base font-bold text-washi-text">視聴方法</h2>
        <p>
          チャンネルの視聴には PeerCast 対応ソフトウェアが必要です。
          お使いのソフトウェアに以下の URL を YP として登録してください。
        </p>
        <table className="w-full border border-washi-border">
          <thead>
            <tr className="text-left border-b border-washi-header bg-washi-surface">
              <th className="py-2 px-4 text-xs font-bold uppercase tracking-wider text-washi-muted">ソフトウェア</th>
              <th className="py-2 px-4 text-xs font-bold uppercase tracking-wider text-washi-muted">登録 URL</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-washi-border text-washi-muted">
            <tr>
              <td className="py-2 px-4 text-sm">PeerCastStation</td>
              <td className="py-2 px-4 font-mono text-sm break-all select-all">{config?.ypIndexURL || '（未設定）'}</td>
            </tr>
            <tr>
              <td className="py-2 px-4 text-sm">
                PeCaRecorder / pcypLite
                <div className="text-xs text-washi-muted font-sans mt-0.5">※ PeCaRecorder は https 非対応です。http で設定してください。</div>
              </td>
              <td className="py-2 px-4 font-mono text-sm break-all select-all">
                {config?.ypIndexURL ? config.ypIndexURL.replace(/\/index\.txt$/, '/') : '（未設定）'}
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section className="space-y-4">
        <h2 className="text-base font-bold text-washi-text">掲載方法</h2>
        <p>
          PeerCastStation の YellowPage 設定の「配信掲載 URL」に以下を登録してください。
        </p>
        <CodeBlock value={config?.pcpAddress} placeholder="（未設定）" />
        <div className="space-y-3">
          <p className="font-bold text-washi-text">ジャンルの設定</p>
          <p className="text-washi-muted">
            ジャンル欄の先頭に <code className="bg-washi-surface font-mono text-sm px-1.5 py-0.5 rounded">{genrePrefix || '（未設定）'}</code> を付けてください。
            付いていないチャンネルはこの YP には掲載されません。
          </p>
          <table className="w-full border border-washi-border">
            <thead>
              <tr className="text-left border-b border-washi-header bg-washi-surface">
                <th className="py-2 px-4 text-xs font-bold uppercase tracking-wider text-washi-muted">設定例</th>
                <th className="py-2 px-4 text-xs font-bold uppercase tracking-wider text-washi-muted">効果</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-washi-border text-washi-muted">
              <tr>
                <td className="py-2 px-4 font-mono text-sm">{genrePrefix ? `${genrePrefix}ゲーム` : '（未設定）'}</td>
                <td className="py-2 px-4 text-sm">ジャンル「ゲーム」で掲載</td>
              </tr>
              <tr>
                <td className="py-2 px-4 font-mono text-sm">{genrePrefix ? `${genrePrefix}?ゲーム` : '（未設定）'}</td>
                <td className="py-2 px-4 text-sm">リスナー数を非表示にして掲載</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div className="space-y-2 bg-washi-surface border border-washi-accent-sub rounded px-4 py-3">
          <p className="font-bold text-washi-header">掲載にあたってのお願い</p>
          <ul className="list-disc list-inside space-y-1 text-washi-text text-sm">
            <li>著作権を侵害するコンテンツの配信はお控えください</li>
            <li>
              掲載には<Link to="/terms" className="underline hover:text-washi-accent">利用規約</Link>への同意が必要です
            </li>
          </ul>
        </div>
      </section>
    </div>
  )
}
