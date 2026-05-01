import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Button, Space, message } from 'antd';
import { CopyOutlined } from '@ant-design/icons';

interface ExpandableTextProps {
  text: string;
  rows?: number;
  fontSize?: number;
  color?: string;
  preWrap?: boolean;
}

export default function ExpandableText({
  text,
  rows = 5,
  fontSize = 12,
  color = 'inherit',
  preWrap = true,
}: ExpandableTextProps) {
  const [expanded, setExpanded] = useState(false);
  const [hasOverflow, setHasOverflow] = useState(false);
  const contentRef = useRef<HTMLDivElement>(null);

  const contentStyle = useMemo(() => {
    const baseStyle = {
      margin: 0,
      fontSize,
      color,
      wordBreak: 'break-word' as const,
      whiteSpace: preWrap ? 'pre-wrap' as const : 'normal' as const,
    };

    if (expanded) {
      return baseStyle;
    }

    return {
      ...baseStyle,
      overflow: 'hidden',
      display: '-webkit-box',
      WebkitBoxOrient: 'vertical' as const,
      WebkitLineClamp: rows,
    };
  }, [color, expanded, fontSize, preWrap, rows]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      message.success('已复制');
    } catch {
      message.error('复制失败');
    }
  };

  useLayoutEffect(() => {
    const element = contentRef.current;
    if (!element) {
      return undefined;
    }

    const measureOverflow = () => {
      const previousDisplay = element.style.display;
      const previousOverflow = element.style.overflow;
      const previousWebkitLineClamp = element.style.webkitLineClamp;
      const previousWebkitBoxOrient = element.style.webkitBoxOrient;

      element.style.display = '-webkit-box';
      element.style.overflow = 'hidden';
      element.style.webkitBoxOrient = 'vertical';
      element.style.webkitLineClamp = String(rows);

      const overflow = element.scrollHeight - element.clientHeight > 1;

      element.style.display = previousDisplay;
      element.style.overflow = previousOverflow;
      element.style.webkitLineClamp = previousWebkitLineClamp;
      element.style.webkitBoxOrient = previousWebkitBoxOrient;

      setHasOverflow(overflow);
      if (!overflow) {
        setExpanded(false);
      }
    };

    measureOverflow();
    window.addEventListener('resize', measureOverflow);

    return () => {
      window.removeEventListener('resize', measureOverflow);
    };
  }, [fontSize, preWrap, rows, text]);

  return (
    <div>
      <div ref={contentRef} style={contentStyle}>{text}</div>
      <Space size={4} style={{ marginTop: 8 }}>
        <Button
          type="link"
          size="small"
          icon={<CopyOutlined />}
          onClick={handleCopy}
          style={{ padding: 0, height: 'auto' }}
        >
          复制
        </Button>
        {hasOverflow && (
          <Button
            type="link"
            size="small"
            onClick={() => setExpanded((value) => !value)}
            style={{ padding: 0, height: 'auto' }}
          >
            {expanded ? '收起' : '展开'}
          </Button>
        )}
      </Space>
    </div>
  );
}
