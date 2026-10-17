import React, { useId } from 'react';

export interface SparklinePoint {
  mmr: number;
}

interface SparklineProps {
  data: Array<SparklinePoint | number>;
  width?: number;
  height?: number;
  className?: string;
}

export const Sparkline: React.FC<SparklineProps> = ({
  data,
  width = 100,
  height = 28,
  className = '',
}) => {
  const gradientId = useId();

  if (!data || data.length === 0) {
    return (
      <svg
        data-testid="sparkline"
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className={`overflow-visible ${className}`}
      >
        <line
          x1={0}
          y1={height / 2}
          x2={width}
          y2={height / 2}
          stroke="currentColor"
          strokeOpacity={0.2}
          strokeDasharray="3 3"
          strokeWidth={1.5}
        />
      </svg>
    );
  }

  const values = data.map((d) => (typeof d === 'number' ? d : d.mmr));

  if (values.length === 1) {
    return (
      <svg
        data-testid="sparkline"
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className={`overflow-visible ${className}`}
      >
        <line
          x1={0}
          y1={height / 2}
          x2={width}
          y2={height / 2}
          stroke="currentColor"
          strokeOpacity={0.2}
          strokeDasharray="3 3"
          strokeWidth={1.5}
        />
        <circle cx={width / 2} cy={height / 2} r={2.5} fill="#64748b" />
      </svg>
    );
  }

  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1;
  const paddingY = 4;
  const usableHeight = height - paddingY * 2;

  const points = values.map((val, idx) => {
    const x = (idx / (values.length - 1)) * width;
    const y = height - paddingY - ((val - min) / range) * usableHeight;
    return { x, y };
  });

  const pathD = points.reduce((acc, pt, idx) => {
    return idx === 0 ? `M ${pt.x.toFixed(1)},${pt.y.toFixed(1)}` : `${acc} L ${pt.x.toFixed(1)},${pt.y.toFixed(1)}`;
  }, '');

  const areaD = `${pathD} L ${width},${height} L 0,${height} Z`;

  const firstVal = values[0];
  const lastVal = values[values.length - 1];
  const delta = lastVal - firstVal;

  const strokeColor = delta > 0 ? '#10b981' : delta < 0 ? '#ef4444' : '#64748b';
  const lastPt = points[points.length - 1];

  return (
    <svg
      data-testid="sparkline"
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={`overflow-visible ${className}`}
    >
      <defs>
        <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={strokeColor} stopOpacity={0.25} />
          <stop offset="100%" stopColor={strokeColor} stopOpacity={0.02} />
        </linearGradient>
      </defs>
      <path d={areaD} fill={`url(#${gradientId})`} />
      <path d={pathD} fill="none" stroke={strokeColor} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
      <circle cx={lastPt.x} cy={lastPt.y} r={2.5} fill={strokeColor} />
    </svg>
  );
};
