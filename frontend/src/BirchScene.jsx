export default function BirchScene() {
  return (
    <svg className="birch-scene" viewBox="0 0 900 620" preserveAspectRatio="xMidYMax slice" aria-hidden="true">
      <defs>
        <radialGradient id="birch-sun" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="#ffffff" stopOpacity=".92" />
          <stop offset="58%" stopColor="#f7f8fa" stopOpacity=".5" />
          <stop offset="100%" stopColor="#f7f8fa" stopOpacity="0" />
        </radialGradient>
        <linearGradient id="birch-trunk" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%" stopColor="#fffefa" />
          <stop offset="52%" stopColor="#f8f7f2" />
          <stop offset="100%" stopColor="#e5e6e2" />
        </linearGradient>
        <linearGradient id="birch-ground" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="#dfe3e8" stopOpacity="0" />
          <stop offset="100%" stopColor="#c5cbd5" stopOpacity=".55" />
        </linearGradient>
      </defs>

      <circle cx="690" cy="152" r="104" fill="url(#birch-sun)" />

      {/* Distant grove */}
      <g fill="none" stroke="#d7dce4" strokeLinecap="round" opacity=".75">
        <path d="M72 620C66 500 70 380 80 270C84 220 86 170 90 130" strokeWidth="5" />
        <path d="M154 620C148 510 152 400 160 300C164 250 166 200 170 160" strokeWidth="4" />
        <path d="M260 620C254 520 258 400 266 300C270 240 272 190 276 140" strokeWidth="6" />
        <path d="M346 620C340 500 344 390 352 290C356 230 358 180 362 120" strokeWidth="4" />
        <path d="M438 620C432 510 436 410 444 310C448 250 450 200 454 150" strokeWidth="3" />
        <path d="M812 620C806 510 810 390 818 280C822 220 824 170 828 120" strokeWidth="6" />
        <path d="M862 620C856 520 860 420 868 320C872 260 874 210 878 160" strokeWidth="3" />
      </g>
      <g fill="none" stroke="#e0e4ea" strokeLinecap="round" opacity=".65">
        <path d="M90 300C118 282 140 262 162 236" strokeWidth="2" />
        <path d="M276 260C304 242 330 224 354 200" strokeWidth="2" />
        <path d="M454 260C480 240 504 218 526 196" strokeWidth="2" />
        <path d="M828 260C800 240 778 222 758 200" strokeWidth="2" />
        <path d="M276 360C304 344 326 326 346 306" strokeWidth="2" />
        <path d="M828 360C802 344 782 326 762 306" strokeWidth="2" />
        <path d="M170 330C194 314 214 298 232 278" strokeWidth="1.6" />
        <path d="M362 300C386 284 408 266 426 246" strokeWidth="1.6" />
      </g>

      <ellipse cx="450" cy="608" rx="440" ry="30" fill="url(#birch-ground)" />
      <g fill="#b3bbc7" opacity=".2">
        <ellipse cx="548" cy="608" rx="30" ry="6" />
        <ellipse cx="632" cy="612" rx="40" ry="7" />
        <ellipse cx="748" cy="614" rx="48" ry="8" />
        <ellipse cx="848" cy="610" rx="32" ry="6" />
      </g>

      {/* Right edge trunk, partly cropped by the frame */}
      <path d="M826 620C822 540 826 460 832 380C836 300 838 240 842 200L860 200C864 260 866 330 864 410C862 480 864 550 868 620Z"
        fill="url(#birch-trunk)" stroke="#c8ced7" strokeWidth="1.2" />
      <path d="M830 600C828 520 832 440 838 370C842 300 844 250 848 216" fill="none" stroke="#ffffff" strokeOpacity=".6" strokeWidth="2" strokeLinecap="round" />
      <g fill="none" stroke="#5c6673" strokeLinecap="round" opacity=".5">
        <path d="M838 544C846 542 854 542 860 543" strokeWidth="2.2" />
        <path d="M836 500C842 499 848 499 853 500" strokeWidth="1.6" />
        <path d="M842 452C850 454 857 453 862 452" strokeWidth="2" />
        <path d="M838 400C844 399 850 400 856 401" strokeWidth="1.5" />
        <path d="M846 352C852 354 858 353 862 352" strokeWidth="1.8" />
        <path d="M842 296C848 295 853 296 858 297" strokeWidth="1.5" />
        <path d="M850 250C855 251 859 250 862 249" strokeWidth="1.6" />
      </g>

      {/* Tallest front trunk */}
      <path d="M714 620C710 520 714 420 722 320C726 240 730 140 734 40L766 40C770 150 772 250 770 340C768 430 770 530 774 620Z"
        fill="url(#birch-trunk)" stroke="#c8ced7" strokeWidth="1.2" />
      <path d="M720 600C716 500 720 400 728 310C732 230 736 140 740 60" fill="none" stroke="#ffffff" strokeOpacity=".65" strokeWidth="2.4" strokeLinecap="round" />
      <g fill="none" stroke="#5c6673" strokeLinecap="round" opacity=".55">
        <path d="M726 566C734 564 744 564 752 565" strokeWidth="2.6" />
        <path d="M722 522C728 520 734 520 740 521M750 518C756 519 762 518 767 517" strokeWidth="1.8" />
        <path d="M730 474C738 476 748 476 756 474" strokeWidth="2.2" />
        <path d="M724 430C730 428 737 428 743 429" strokeWidth="1.6" />
        <path d="M734 386C742 388 750 388 757 386" strokeWidth="2.4" />
        <path d="M728 340C734 338 741 339 747 340" strokeWidth="1.5" />
        <path d="M738 294C745 296 752 295 758 294" strokeWidth="2" />
        <path d="M732 246C738 245 744 245 749 246" strokeWidth="1.6" />
        <path d="M742 198C749 199 755 198 760 197" strokeWidth="2" />
        <path d="M736 150C742 149 747 150 752 151" strokeWidth="1.5" />
        <ellipse cx="754" cy="448" rx="4" ry="2.6" fill="#5c6673" stroke="none" />
        <ellipse cx="724" cy="372" rx="3" ry="2" fill="#5c6673" stroke="none" />
      </g>

      {/* Middle trunk */}
      <path d="M606 620C602 540 606 450 612 360C616 280 618 200 622 120L646 120C650 200 652 300 650 390C648 470 650 550 656 620Z"
        fill="url(#birch-trunk)" stroke="#c8ced7" strokeWidth="1.2" />
      <path d="M612 600C608 520 612 440 618 360C622 280 624 200 628 132" fill="none" stroke="#ffffff" strokeOpacity=".6" strokeWidth="2" strokeLinecap="round" />
      <g fill="none" stroke="#5c6673" strokeLinecap="round" opacity=".5">
        <path d="M618 548C626 546 634 546 641 547" strokeWidth="2.2" />
        <path d="M614 504C620 503 626 503 632 504" strokeWidth="1.6" />
        <path d="M622 458C630 460 638 459 644 458" strokeWidth="2" />
        <path d="M616 412C622 411 628 412 634 413" strokeWidth="1.5" />
        <path d="M626 366C633 368 640 367 646 366" strokeWidth="2.2" />
        <path d="M620 318C626 317 632 318 637 319" strokeWidth="1.5" />
        <path d="M630 272C636 274 642 273 647 272" strokeWidth="1.8" />
        <path d="M624 226C630 225 635 226 640 227" strokeWidth="1.5" />
        <path d="M634 184C640 185 645 184 650 183" strokeWidth="1.7" />
      </g>

      {/* Foreground short trunk */}
      <path d="M528 620C526 560 528 500 532 440C534 410 536 380 540 360L558 360C562 390 564 430 562 470C560 530 562 580 566 620Z"
        fill="url(#birch-trunk)" stroke="#c8ced7" strokeWidth="1.2" />
      <path d="M534 612C532 554 534 496 538 440C540 410 542 384 545 372" fill="none" stroke="#ffffff" strokeOpacity=".55" strokeWidth="1.6" strokeLinecap="round" />
      <g fill="none" stroke="#5c6673" strokeLinecap="round" opacity=".45">
        <path d="M538 566C544 565 550 565 555 566" strokeWidth="1.8" />
        <path d="M536 526C541 525 546 525 551 526" strokeWidth="1.4" />
        <path d="M542 484C548 486 553 485 557 484" strokeWidth="1.7" />
        <path d="M538 442C543 441 548 442 553 443" strokeWidth="1.3" />
        <path d="M544 402C549 404 553 403 557 402" strokeWidth="1.6" />
      </g>

      {/* Branches */}
      <g fill="none" stroke="#b9c1cb" strokeLinecap="round">
        <path d="M740 266C714 242 694 218 672 190" strokeWidth="2.2" />
        <path d="M756 318C782 300 804 282 824 258" strokeWidth="2" />
        <path d="M754 150C738 126 724 102 712 76" strokeWidth="1.8" />
        <path d="M760 150C776 126 790 104 802 82" strokeWidth="1.7" />
        <path d="M620 302C594 278 574 254 552 224" strokeWidth="1.9" />
        <path d="M642 414C666 394 686 376 704 354" strokeWidth="1.7" />
        <path d="M842 356C862 338 878 320 890 300" strokeWidth="1.6" />
        <path d="M544 476C524 458 508 440 496 420" strokeWidth="1.4" />
        <path d="M450 316C476 296 498 278 518 256" strokeWidth="1.4" stroke="#c4cbd5" />
        <path d="M824 420C846 404 864 388 878 370" strokeWidth="1.5" stroke="#c4cbd5" />
      </g>

      {/* Sage leaf clusters */}
      <g opacity=".8">
        <g fill="#a9c0b4">
          <ellipse cx="664" cy="184" rx="11" ry="4" transform="rotate(-30 664 184)" />
          <ellipse cx="680" cy="176" rx="9" ry="3.4" transform="rotate(12 680 176)" />
          <ellipse cx="670" cy="198" rx="8" ry="3.2" transform="rotate(58 670 198)" />
          <ellipse cx="692" cy="188" rx="7.5" ry="3" transform="rotate(28 692 188)" />
        </g>
        <g fill="#bdcfc6">
          <ellipse cx="544" cy="218" rx="10" ry="3.8" transform="rotate(-22 544 218)" />
          <ellipse cx="558" cy="208" rx="8" ry="3.2" transform="rotate(18 558 208)" />
          <ellipse cx="532" cy="230" rx="7" ry="2.8" transform="rotate(42 532 230)" />
        </g>
        <g fill="#9db8ab">
          <ellipse cx="828" cy="254" rx="10" ry="3.8" transform="rotate(-28 828 254)" />
          <ellipse cx="842" cy="244" rx="8" ry="3.2" transform="rotate(10 842 244)" />
          <ellipse cx="816" cy="264" rx="7" ry="2.8" transform="rotate(36 816 264)" />
        </g>
        <g fill="#b3c7bb">
          <ellipse cx="706" cy="72" rx="8" ry="3.2" transform="rotate(-34 706 72)" />
          <ellipse cx="718" cy="64" rx="7" ry="2.8" transform="rotate(2 718 64)" />
          <ellipse cx="806" cy="78" rx="8" ry="3.2" transform="rotate(28 806 78)" />
          <ellipse cx="794" cy="66" rx="6.5" ry="2.6" transform="rotate(-6 794 66)" />
        </g>
        <g fill="#c3d3ca">
          <ellipse cx="702" cy="352" rx="9" ry="3.4" transform="rotate(-24 702 352)" />
          <ellipse cx="714" cy="344" rx="7" ry="2.8" transform="rotate(14 714 344)" />
          <ellipse cx="690" cy="362" rx="6" ry="2.4" transform="rotate(38 690 362)" />
        </g>
        <g fill="#b0c5b9">
          <ellipse cx="494" cy="416" rx="8" ry="3" transform="rotate(-18 494 416)" />
          <ellipse cx="506" cy="408" rx="6.5" ry="2.6" transform="rotate(22 506 408)" />
          <ellipse cx="874" cy="366" rx="8" ry="3" transform="rotate(-26 874 366)" />
          <ellipse cx="886" cy="356" rx="6.5" ry="2.6" transform="rotate(8 886 356)" />
        </g>
        <g fill="#b8cabf" opacity=".75">
          <ellipse cx="424" cy="272" rx="7" ry="2.8" transform="rotate(-20 424 272)" />
          <ellipse cx="436" cy="264" rx="6" ry="2.4" transform="rotate(16 436 264)" />
          <ellipse cx="572" cy="246" rx="7" ry="2.8" transform="rotate(32 572 246)" />
        </g>
      </g>

      {/* Ground grass */}
      <g fill="none" stroke="#b8c0ca" strokeLinecap="round" opacity=".65">
        <path d="M344 608C348 596 352 588 358 580M360 610C356 600 354 592 350 584" strokeWidth="1.6" />
        <path d="M464 612C468 600 472 592 478 584M480 614C476 604 474 596 470 588" strokeWidth="1.4" />
        <path d="M584 614C588 602 592 594 598 586" strokeWidth="1.6" />
        <path d="M688 616C692 604 696 596 702 588M704 618C700 608 698 600 694 592" strokeWidth="1.5" />
        <path d="M808 616C812 604 816 596 822 588" strokeWidth="1.4" />
        <path d="M870 612C874 600 878 592 884 584M886 614C882 604 880 596 876 588" strokeWidth="1.5" />
      </g>
    </svg>
  );
}
