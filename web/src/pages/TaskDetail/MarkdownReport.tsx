import Markdown from 'react-markdown'

export default function MarkdownReport({ content }: { content: string }) {
  return <Markdown>{content}</Markdown>
}
