import React from 'react';
import { PlatformDisplayMode, useDashboard } from '../../context/DashboardContext';
import { parsePlatform, PlatformCode } from '../../utils/platforms';

export interface PlatformIconProps {
  code: PlatformCode;
  className?: string;
}

const SIZE_CLASSES = {
  xs: 'w-3.5 h-3.5',
  sm: 'w-4 h-4',
  md: 'w-[18px] h-[18px]',
} as const;

export const PlatformIcon: React.FC<PlatformIconProps> = ({ code, className = 'w-4 h-4' }) => {
  switch (code) {
    case 'steam':
      return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
          <path d="M11.979 0C5.678 0 .511 4.86.022 11.037l6.432 2.658c.545-.371 1.203-.59 1.912-.59.063 0 .125.004.188.006l2.861-4.142V8.91c0-2.495 2.028-4.524 4.524-4.524 2.494 0 4.524 2.031 4.524 4.527s-2.03 4.525-4.524 4.525h-.105l-4.076 2.911c0 .052.004.105.004.159 0 1.875-1.515 3.396-3.39 3.396-1.635 0-3.016-1.173-3.331-2.727L.436 15.27C1.862 20.307 6.486 24 11.979 24c6.627 0 11.999-5.373 11.999-12S18.605 0 11.979 0zM7.54 18.21l-1.473-.61c.262.543.714.999 1.314 1.25 1.297.539 2.793-.076 3.332-1.375.263-.63.264-1.319.005-1.949s-.75-1.121-1.377-1.383c-.624-.26-1.29-.249-1.878-.03l1.523.63c.956.4 1.409 1.5 1.009 2.455-.397.957-1.497 1.41-2.454 1.012H7.54zm11.415-9.303c0-1.662-1.353-3.015-3.015-3.015-1.665 0-3.015 1.353-3.015 3.015 0 1.665 1.35 3.015 3.015 3.015 1.663 0 3.015-1.35 3.015-3.015zm-5.273-.005c0-1.252 1.013-2.266 2.265-2.266 1.249 0 2.266 1.014 2.266 2.266 0 1.251-1.017 2.265-2.266 2.265-1.253 0-2.265-1.014-2.265-2.265z" />
        </svg>
      );
    case 'epic':
      return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
          <path
            fillRule="evenodd"
            clipRule="evenodd"
            d="M4.2 1.2h15.6c.94 0 1.7.76 1.7 1.7v13.3c0 .95-.52 1.82-1.35 2.27L12 22.8l-8.15-4.33A2.57 2.57 0 0 1 2.5 16.2V2.9c0-.94.76-1.7 1.7-1.7zm3.4 3.4v11h8.8v-2.4h-6.2v-2h5.1V9h-5.1V7h6.2V4.6H7.6zm1.2 12.6v1.4h6.4v-1.4H8.8z"
          />
        </svg>
      );
    case 'psn':
      return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
          <path d="M8.985 2.596v17.548l3.915 1.261V6.688c0-.69.304-1.151.794-.991.636.181.76.814.76 1.505v5.876c2.441 1.193 4.362-.002 4.362-3.153 0-3.237-1.126-4.675-4.438-5.827-1.307-.448-3.728-1.186-5.393-1.502zm4.656 16.242l6.296-2.275c.715-.258.826-.625.246-.818-.586-.192-1.637-.139-2.357.123l-4.205 1.495v-2.385l.24-.085s1.201-.42 2.913-.615c1.696-.18 3.785.03 5.437.661 1.848.601 2.04 1.44 1.576 2.072-.465.632-1.605 1.074-1.605 1.074l-8.541 3.091v-2.338zM1.807 18.6c-1.901-.545-2.215-1.668-1.352-2.32.801-.586 2.16-1.052 2.16-1.052l5.619-2.013v2.313l-4.05 1.452c-.705.271-.825.632-.24.819.586.187 1.638.133 2.358-.126l1.932-.706v2.074c-.12.03-.256.046-.39.075-1.936.316-3.991.185-6.037-.516z" />
        </svg>
      );
    case 'xbox':
      return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
          <path d="M4.102 21.033C6.211 22.881 8.977 24 12 24c3.026 0 5.789-1.119 7.902-2.967 1.877-1.912-4.316-8.709-7.902-11.417-3.582 2.708-9.779 9.505-7.898 11.417zm11.16-14.406c2.5 2.961 7.484 10.313 6.076 12.912 1.667-2.075 2.662-4.716 2.662-7.539 0-4.124-2.08-7.76-5.252-9.914-.043-.028-.14-.051-.27-.051-.724 0-2.011.613-3.216 4.592zm-6.526 0C7.531 2.648 6.244 2.035 5.52 2.035c-.13 0-.227.023-.27.051C2.08 4.24 0 7.876 0 12c0 2.823.995 5.464 2.662 7.539-1.408-2.599 3.576-9.951 6.074-12.912zM12 0C9.736 0 7.619.634 5.815 1.735c.869-.102 2.645.289 6.185 2.87 3.54-2.581 5.316-2.972 6.185-2.87C16.381.634 14.264 0 12 0z" />
        </svg>
      );
    case 'switch':
      return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
          <path
            fillRule="evenodd"
            clipRule="evenodd"
            d="M14.176 24h3.674c3.376 0 6.15-2.774 6.15-6.15V6.15C24 2.775 21.226 0 17.85 0h-3.675c-.15 0-.225.075-.225.225v23.55c0 .15.075.225.225.225zm4.574-13.199c1.351 0 2.399 1.125 2.399 2.398 0 1.276-1.125 2.4-2.399 2.4-1.35 0-2.4-1.049-2.4-2.4 0-1.274 1.05-2.398 2.4-2.398zM10.049 0H6.15C2.775 0 0 2.775 0 6.15v11.7C0 21.226 2.775 24 6.15 24h3.899c.151 0 .225-.075.225-.225V.225C10.274.075 10.199 0 10.049 0zM8.325 22.05H6.15c-2.325 0-4.2-1.875-4.2-4.2V6.15c0-2.325 1.875-4.2 4.2-4.2h2.175v20.1zM5.625 9.974c1.275 0 2.325-1.05 2.325-2.325s-1.05-2.325-2.325-2.325S3.3 6.374 3.3 7.649s1.05 2.325 2.325 2.325z"
          />
        </svg>
      );
    case 'psynet':
      return (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" className={className}>
          <circle cx="12" cy="12" r="9.5" />
          <path d="M2.5 12h19" />
          <path d="M12 2.5a14 14 0 0 1 0 19a14 14 0 0 1 0-19z" />
        </svg>
      );
    case 'bot':
      return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
          <path
            fillRule="evenodd"
            clipRule="evenodd"
            d="M12 1.5a1.5 1.5 0 0 1 1 2.62V6h5a3 3 0 0 1 3 3v9a3 3 0 0 1-3 3H6a3 3 0 0 1-3-3V9a3 3 0 0 1 3-3h5V4.12A1.5 1.5 0 0 1 12 1.5zM8.5 11a1.75 1.75 0 1 0 0 3.5 1.75 1.75 0 0 0 0-3.5zm7 0a1.75 1.75 0 1 0 0 3.5 1.75 1.75 0 0 0 0-3.5zm-7 5.5v1.6h7v-1.6h-7z"
          />
        </svg>
      );
    default:
      return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className={className}>
          <path
            fillRule="evenodd"
            clipRule="evenodd"
            d="M6.8 6h10.4c2.4 0 4.3 1.7 4.7 4l1 5.6c.3 1.7-1 3.4-2.8 3.4-1 0-1.9-.5-2.5-1.3l-1.5-2.2H7.9l-1.5 2.2c-.6.8-1.5 1.3-2.5 1.3-1.8 0-3.1-1.7-2.8-3.4l1-5.6C2.5 7.7 4.4 6 6.8 6zm1.7 3.2v1.6H6.9v1.6h1.6V14h1.6v-1.6h1.6v-1.6h-1.6V9.2H8.5zm6.4.4a1.1 1.1 0 1 0 0 2.2 1.1 1.1 0 0 0 0-2.2zm2.4 2.2a1.1 1.1 0 1 0 0 2.2 1.1 1.1 0 0 0 0-2.2z"
          />
        </svg>
      );
  }
};

export interface PlatformBadgeProps {
  platform?: string | null;
  isBot?: boolean;
  /** Optional display override; defaults to the Settings `platformDisplay` preference. */
  displayMode?: PlatformDisplayMode;
  size?: keyof typeof SIZE_CLASSES;
  className?: string;
}

export const PlatformBadge: React.FC<PlatformBadgeProps> = ({
  platform,
  isBot = false,
  displayMode,
  size = 'sm',
  className = '',
}) => {
  const { platformDisplay } = useDashboard();
  const mode = displayMode ?? platformDisplay;
  const info = parsePlatform(platform, isBot);

  if (!isBot && !(platform || '').trim()) {
    return (
      <span
        data-testid="platform-badge"
        data-platform="unknown"
        data-platform-display={mode}
        className={className || undefined}
      >
        —
      </span>
    );
  }

  if (mode === 'name') {
    return (
      <span
        data-testid="platform-badge"
        data-platform={info.code}
        data-platform-display="name"
        title={info.name}
        className={className || undefined}
      >
        {info.shortLabel}
      </span>
    );
  }

  return (
    <span
      data-testid="platform-badge"
      data-platform={info.code}
      data-platform-display="icon"
      title={info.name}
      aria-label={info.name}
      className={`inline-flex items-center justify-center align-middle shrink-0 ${className}`.trim()}
    >
      <PlatformIcon code={info.code} className={SIZE_CLASSES[size]} />
      <span className="sr-only">{info.shortLabel}</span>
    </span>
  );
};
